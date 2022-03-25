package memolock

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gofrs/uuid"
)

// FetchFunc is the function that the caller should provide to compute the value if not present in Redis already.
// time.Duration defines for how long the value should be cached in Redis.
type FetchFunc = func() (string, time.Duration, error)

// ExternalFetchFunc has the same purpose as FetchFunc but works on the assumption that the value will be set in Redis and notificed on Pub/Sub by an external program
type ExternalFetchFunc = func() error

// LockRenewFunc is the function that RenewableFetchFunc will get as input and that must be called to extend a locks' life
type LockRenewFunc = func(time.Duration) error

// RenewableFetchFunc has the same purpose as FetchFunc but, when called, it is offered a function that allows to extend the lock
type RenewableFetchFunc = func(LockRenewFunc) (string, time.Duration, error)

// ErrTimeOut happens when the given timeout expires
var ErrTimeOut = errors.New("Operation Timed Out")

// ErrLockRenew happens when trying to renew a lock that expired already
var ErrLockRenew = errors.New("Unable to renew the lock")

// ErrClosing happens when calling Close(), all pending requests will be failed with this error
var ErrClosing = errors.New("Operation canceled by Close()")

const renewLockLuaScript = `
    if redis.call('GET', KEYS[1]) == ARGV[1]
    then 
        redis.call('EXPIRE', KEYS[1], ARGV[2]) 
        return 1
    else 
        return 0
    end
`

// Order is not maintained by this method, but it is fast
func remove(s []chan string, i int) []chan string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

type subRequest struct {
	name    string
	isUnsub bool
	resCh   chan string
}

// RedisMemoLock implements the "promise" mechanism
type RedisMemoLock struct {
	client        *redis.Client
	resourceTag   string
	lockTimeout   time.Duration
	subCh         chan subRequest
	notifCh       <-chan *redis.Message
	subscriptions map[string][]chan string
}

func (r *RedisMemoLock) dispatch() {
	for {
		select {
		// We have a new sub/unsub request
		case sub, ok := <-r.subCh:
			if !ok { // TODO: When could this happen???
				// We are closing, close all pending channels
				for _, list := range r.subscriptions {
					for _, ch := range list {
						close(ch)
					}
				}
				return
			}

			switch sub.isUnsub {
			case false:
				list, _ := r.subscriptions[sub.name]
				r.subscriptions[sub.name] = append(list, sub.resCh)
			case true:

				// Order of the subscriptions is not maintained by this
				// that shouldn't matter but noting it here in case it does...
				list, ok := r.subscriptions[sub.name]
				index := 0
				if ok {
					for i, resultChannel := range list {
						if sub.resCh == resultChannel {
							index = i
						}
					}

					unsubbedList := remove(list, index)

					// If the list of channels is empty we should remove the subscription altogether
					// it will be recreated if someone else subscribes
					if len(unsubbedList) == 0 {
						delete(r.subscriptions, sub.name)
					} else {
						// if more channels remain then we just remove the current unsub channel from list
						r.subscriptions[sub.name] = unsubbedList
					}
				}
			}
		// We have a new notification from Redis Pub/Sub
		case msg := <-r.notifCh:
			if listeners, ok := r.subscriptions[msg.Channel]; ok {
				for _, ch := range listeners {
					ch <- msg.Payload
					close(ch)
				}

				delete(r.subscriptions, msg.Channel)
			}
		}
	}
}

// NewRedisMemoLock Creates a new RedisMemoLock instance
func NewRedisMemoLock(client *redis.Client, resourceTag string, lockTimeout time.Duration) (*RedisMemoLock, error) {
	pattern := resourceTag + "/notif:*"

	pubsub := client.PSubscribe(context.Background(), pattern)
	_, err := pubsub.Receive(context.Background())
	if err != nil {
		return nil, err
	}

	result := RedisMemoLock{
		client:        client,
		resourceTag:   resourceTag,
		lockTimeout:   lockTimeout,
		subCh:         make(chan subRequest),
		notifCh:       pubsub.Channel(),
		subscriptions: make(map[string][]chan string),
	}

	// Start the dispatch loop
	go result.dispatch()

	return &result, nil
}

// Close stops listening to Pub/Sub and resolves all pending subscriptions with ErrClosing.
func (r *RedisMemoLock) Close() {
	close(r.subCh)
}

// GetResource tries to get a resource from Redis, resorting to call generatingFunc in case of a cache miss
func (r *RedisMemoLock) GetResource(resID string, timeout time.Duration, generatingFunc FetchFunc) (string, error) {
	reqUUID := uuid.Must(uuid.NewV4()).String()
	return r.getResourceImpl(resID, generatingFunc, timeout, reqUUID, false)
}

func (r *RedisMemoLock) lockRenewFuncGenerator(lockID string, reqUUID string) LockRenewFunc {
	return func(extension time.Duration) error {
		cmd := r.client.Eval(context.Background(), renewLockLuaScript, []string{lockID}, reqUUID, int(extension))
		if err := cmd.Err(); err != nil {
			return err
		}
		// Were we still owning the lock when we tried to extend it?
		val, err := cmd.Bool()
		if err != nil {
			return err
		}

		if val {
			return nil
		}

		return ErrLockRenew
	}
}

// GetResourceRenewable has the same purpose as GetResource but allows the caller to extend the lock lease during the execution of generatingFunc
func (r *RedisMemoLock) GetResourceRenewable(resID string, timeout time.Duration, generatingFunc RenewableFetchFunc) (string, error) {
	reqUUID := uuid.Must(uuid.NewV4()).String()
	lockID := r.resourceTag + "/lock:" + resID

	// We now prepare a wrapper that injects a lock-extending function
	// to the one provided by the caller.
	injectedFunc := func() (string, time.Duration, error) {
		return generatingFunc(r.lockRenewFuncGenerator(lockID, reqUUID))
	}

	return r.getResourceImpl(resID, injectedFunc, timeout, reqUUID, false)
}

// GetResourceExternal assumes that the value will be set on Redis and notified on Pub/Sub by another program.
// Useful for when generatingFunc launches an executable instead of doing the work in the current context.
func (r *RedisMemoLock) GetResourceExternal(resID string, timeout time.Duration, generatingFunc ExternalFetchFunc) (string, error) {
	reqUUID := uuid.Must(uuid.NewV4()).String()
	wrappedFunc := func() (string, time.Duration, error) {
		return "", 0, generatingFunc()
	}
	return r.getResourceImpl(resID, wrappedFunc, timeout, reqUUID, true)
}

func (r *RedisMemoLock) getResourceImpl(resID string, generatingFunc FetchFunc, timeout time.Duration, reqUUID string, externallyManaged bool) (string, error) {
	resourceID := r.resourceTag + ":" + resID
	lockID := r.resourceTag + "/lock:" + resID
	notifID := r.resourceTag + "/notif:" + resID

	// If the resource is available, return it immediately.
	res, err := r.client.Get(context.Background(), resourceID).Result()
	if err != redis.Nil { // key is not missing
		if err != nil { // real error happened?
			return "", err
		}
		return res, nil
	}
	// key is missing

	// The resource is not available, can we get the lock?
	resourceLock, err := r.client.SetNX(context.Background(), lockID, reqUUID, r.lockTimeout).Result()
	if err != nil {
		return "", err
	}

	if resourceLock {
		// It's possible, though very very unlikely, that we could enter this _after_ a failed Get
		// and _also after_ the cached value is Set in Redis (see the code below)
		// We want to avoid generating the value if we can, and so we perform an extra Get
		// here to check if it has been set in the brief window that exists for it to occur.
		//
		// Note on performance: Yes this is an extra call and that's not nothing, but we already
		//   do two Gets in the case where the lock has already been acquired, so an additional
		//   one here shouldn't hurt. We hit the other case significantly more often.
		res, err := r.client.Get(context.Background(), resourceID).Result()
		if err != redis.Nil { // key is not missing
			if err != nil { // real error happened?
				return "", err
			}

			// The lock we acquired is not valid because we're not generating so we must relinquish it
			// While doing so we might as well Publish just in case other callers are subscribed now
			// They would otherwise timeout if we didn't publish.
			pipe := r.client.Pipeline()
			pipe.Publish(context.Background(), notifID, res)
			pipe.Del(context.Background(), lockID) // We need to relinquish the lock after we're done, otherwise it will persist until expiration
			_, err := pipe.Exec(context.Background())
			if err != nil {
				return "", err
			}
			return res, nil
		}

		// We acquired the lock, use the client-provided func to generate the resource.
		resourceValue, resourceTTL, err := generatingFunc()
		if err != nil {
			return "", err
		}

		if !externallyManaged {
			// Storage of the token on Redis and notification is handled
			// by us and we can return the token immediately.
			// We need to relinquish the lock after we're done here.
			// If we don't it will persist until the `lockTimeout` has expired
			// When we invalidate cache values we can encounter issues with this
			// because the lock exists but there is nothing generating a value.
			pipe := r.client.Pipeline()
			pipe.Set(context.Background(), resourceID, resourceValue, resourceTTL)
			pipe.Publish(context.Background(), notifID, resourceValue)
			pipe.Del(context.Background(), lockID) // We need to relinquish the lock after we're done, otherwise it will persist until expiration
			_, err := pipe.Exec(context.Background())
			if err != nil {
				return "", err
			}
			return resourceValue, nil
		}

		// The notification will be created by an external system
		// so we falltrough and subscribe to notifs anyway.
	}

	// The resource is not ready yet so we wait for a notification of completion.
	subReq := subRequest{name: notifID, isUnsub: false, resCh: make(chan string, 1)}

	// resCh needs to have buffering to make sure it does not
	// lock the dispatch goroutine when timeouts happen.

	// Send a request to sub
	r.subCh <- subReq
	unsubRequest := subRequest{name: notifID, isUnsub: true, resCh: subReq.resCh}

	// Refetch the key in case we missed the pubsub announcement by a hair.
	// subCh must have no buffering to make sure we do this *after* the sub
	// really takes effect.
	res, err = r.client.Get(context.Background(), resourceID).Result()
	if err != redis.Nil { // key is not missing
		if err != nil { // real error happened?
			r.subCh <- unsubRequest
			return "", err
		}
		r.subCh <- unsubRequest
		return res, nil
	}
	// key is missing

	select {
	// On timeout, remove the subscription and return a timeout error.
	case <-time.After(timeout):
		r.subCh <- unsubRequest
		return "", ErrTimeOut
	// The request can fail if .Close() was called
	case res, ok := <-subReq.resCh:
		if !ok {
			return "", ErrClosing
		}
		return res, nil
	}
}
