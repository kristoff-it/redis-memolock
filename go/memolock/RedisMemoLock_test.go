package memolock

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var client *redis.Client
var redisMemoLock *RedisMemoLock

func TestMain(m *testing.M) {
	var err error
	port := "6379/tcp"

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:latest",
			ExposedPorts: []string{port},

			WaitingFor: wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	}

	container, err := testcontainers.GenericContainer(context.Background(), req)
	if err != nil {
		log.Fatal(err)
		return
	}

	externalPort, err := container.MappedPort(context.Background(), nat.Port(port))
	if err != nil {
		log.Fatal(err)
		return
	}

	useExternalPort := strings.Split(string(externalPort), "/")

	os.Setenv("REDIS_URI", "localhost:"+string(useExternalPort[0]))

	client = redis.NewClient(&redis.Options{
		Addr: "localhost:" + string(useExternalPort[0]),
	})

	queryMemolock, err := NewRedisMemoLock(client, "test", 5*time.Second)
	if err != nil {
		log.Fatal(err)
		return
	}

	redisMemoLock = queryMemolock

	code := m.Run()
	os.Exit(code)
}

func TestRedisMemoLock(t *testing.T) {

	t.Run("A Get to a resource already being generated should wait and not generate its own resource", func(t *testing.T) {
		resourceId := uuid.NewString()
		value := uuid.NewString()

		var wg sync.WaitGroup

		assert.Equal(t, len(redisMemoLock.subscriptions), 0)

		// Initial Get of uncached resource, this will generate the value
		var result string
		wg.Add(1)
		go func() {
			s, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
				time.Sleep(2 * time.Second)

				return value, 5 * time.Second, nil
			})
			if err != nil {
				panic("failed")
			}

			result = s
			wg.Done()
		}()

		// Need to wait a bit before the second Get otherwise it could win in acquiring lock
		time.Sleep(50 * time.Millisecond)

		// Second get to the same resource should _not_ generate the resource and instead wait to be notified of the generated value
		var resultTwo string
		wg.Add(1)
		go func() {
			s, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
				return "should not happen because value is being cached", 5 * time.Second, nil
			})
			if err != nil {
				panic("failed")
			}

			resultTwo = s
			wg.Done()
		}()

		// Need to wait again to ensure the second Get has subscribed
		time.Sleep(50 * time.Millisecond)

		// Since the second Get is subscribed we should see it in the list here
		assert.Equal(t, 1, len(redisMemoLock.subscriptions))

		// Wait for both to finish so we can verify the returned values are what we expect
		wg.Wait()
		assert.Equal(t, value, result)
		assert.Equal(t, value, resultTwo)
	})

	// This test ensures that a process that has timed out will be removed from the subscription list - and since it is the only
	// channel within that list the key/value should be deleted from the subscription map altogether
	t.Run("a process waiting to be notified should be properly unsubscribed if it times out waiting for the value", func(t *testing.T) {
		resourceId := uuid.NewString()
		value := uuid.NewString()

		var wg sync.WaitGroup

		assert.Equal(t, len(redisMemoLock.subscriptions), 0)

		// Initial Get of uncached resource, this will generate the value
		var result string
		wg.Add(1)
		go func() {
			s, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
				time.Sleep(2 * time.Second)

				return value, 2 * time.Second, nil
			})
			assert.Nil(t, err)

			result = s
			wg.Done()
		}()

		// Need to wait a bit before the second Get otherwise it could win in acquiring lock
		time.Sleep(50 * time.Millisecond)

		// Second get to the same resource should _not_ generate the resource and instead wait to be notified of the generated value
		// var resultTwo string
		wg.Add(1)
		go func() {
			_, err := redisMemoLock.GetResource(resourceId, 1*time.Second, func() (string, time.Duration, error) {
				return "should not happen because value is being cached", 5 * time.Second, nil
			})
			assert.NotNil(t, err)

			wg.Done()
		}()

		// Need to wait again to ensure the second Get has subscribed
		time.Sleep(50 * time.Millisecond)

		// Since the second Get is subscribed we should see it in the list here
		assert.Equal(t, 1, len(redisMemoLock.subscriptions))

		// Wait for both to finish so we can verify the returned values are what we expect
		wg.Wait()
		assert.Equal(t, 0, len(redisMemoLock.subscriptions))

		assert.Equal(t, value, result)
	})

	// This is a comprehensive test that verifies a few things
	// - First value to access an uncached resource will generate the value
	// - Other processes accessing the same value will wait to be notified, adding themselves to the subscription list
	// - A process that times out waiting to be notified will properly unsub itself, but the subscription won't be
	//   completely removed if there are other channels waiting for a notification
	// - A notification of a generated value is received and is the correct value, and the subscription is removed
	//   once all the notifications have been sent out
	t.Run("generating, subscribing, and notifying and cleanup all work as expected with multiple processes", func(t *testing.T) {
		resourceId := uuid.NewString()
		value := uuid.NewString()

		var wg sync.WaitGroup

		assert.Equal(t, len(redisMemoLock.subscriptions), 0)

		// Initial Get of uncached resource, this will generate the value
		var result string
		wg.Add(1)
		go func() {
			s, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
				time.Sleep(2 * time.Second)

				return value, 2 * time.Second, nil
			})
			assert.Nil(t, err)

			result = s
			wg.Done()
		}()

		// Need to wait a bit before the second Get otherwise it could win in acquiring lock
		time.Sleep(50 * time.Millisecond)

		// This one will time out
		timeoutWg := sync.WaitGroup{}
		timeoutWg.Add(1)
		go func() {
			_, err := redisMemoLock.GetResource(resourceId, 1*time.Second, func() (string, time.Duration, error) {
				return "should not happen because value is being cached", 1 * time.Second, nil
			})
			assert.NotNil(t, err)

			timeoutWg.Done()
		}()

		// This one will not time out
		var notifiedResult string
		wg.Add(1)
		go func() {
			s, err := redisMemoLock.GetResource(resourceId, 2*time.Second+300*time.Millisecond, func() (string, time.Duration, error) {
				return "should not happen because value is being cached", 1 * time.Second, nil
			})
			assert.Nil(t, err)

			notifiedResult = s
			wg.Done()
		}()

		// Need to wait again to ensure the follow-up Gest have subscribed
		time.Sleep(50 * time.Millisecond)

		// We should see one subscription and two channels waiting to be notified
		assert.Equal(t, 1, len(redisMemoLock.subscriptions))
		channels := redisMemoLock.subscriptions["test/notif:"+resourceId]
		assert.Equal(t, 2, len(channels))

		timeoutWg.Wait()
		// The dispatch is an async process so we give it a little time to process the unsub request
		time.Sleep(10 * time.Millisecond)

		//Recheck after the timeout has happened
		assert.Equal(t, 1, len(redisMemoLock.subscriptions))
		channels = redisMemoLock.subscriptions["test/notif:"+resourceId]
		assert.Equal(t, 1, len(channels))

		// Wait for the rest to finish so we can verify the returned values are what we expect
		wg.Wait()
		assert.Equal(t, 0, len(redisMemoLock.subscriptions))

		assert.Equal(t, value, result)
		assert.Equal(t, value, notifiedResult)
	})

	// This test is attempting to ensure that the lock that is acquired to generate a value that we cache
	// is relinquished after the generation of that value has taken place. The original code would SETNX a lock
	// and ultimately wait for it to expire. This created issues when we attempted to invalidate the generated
	// value. If we have a lockTimeout of say 5 seconds and this happens before 5 seconds occurs we have issues:
	// `GetResource` -> Delete the cached value -> `GetResource`(again, could be a new request from a totally different service)
	// In this scenario this happens for the second `GetResource`
	// - Redis.Get, doesn't find value because it was deleted
	// - Attempt to acquire lock, fail because the lock is still held by the original `GetResource` because it hasn't expired
	// - Subscribe to the Redis pub/sub and never receive a value, ultimately timing out and erroring.
	// This test recreates this scenario and originally failed when written. It passing means that at least in this case the lock
	// is relinquished once the resource is generated so the second `GetResource` can function properly.
	t.Run("we should succeed in generating a value on a follow-up Get even if the lockTimeout duration hasn't passed", func(t *testing.T) {
		resourceId := uuid.NewString()
		value := uuid.NewString()
		value2 := uuid.NewString()

		// Initial Get of uncached resource, this will generate the value
		result, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
			time.Sleep(2 * time.Second)

			return value, 5 * time.Second, nil
		})
		assert.Nil(t, err)

		// Delete the value so that we can ensure the lock is relinquished when we perform a follow-up get
		_, err = client.Del(context.Background(), fmt.Sprintf("test:%s", resourceId)).Result()
		assert.Nil(t, err)

		// Follow-up Get that should not timeout and instead cache `value2`
		resultTwo, err := redisMemoLock.GetResource(resourceId, 5*time.Second, func() (string, time.Duration, error) {
			return value2, 5 * time.Second, nil
		})
		assert.Nil(t, err)

		assert.Equal(t, value, result)
		assert.Equal(t, value2, resultTwo)
	})
}
