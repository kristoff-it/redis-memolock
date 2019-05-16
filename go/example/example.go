package main

import (
    "fmt"
    "time"
    "github.com/go-redis/redis"
    "github.com/kristoff-it/redis-memolock/go/memolock"
)

func main () {
    // First, we need a redis connection pool:
    r := redis.NewClient(&redis.Options{
        Addr:     "localhost:6379", // use default Addr
        Password: "",               // no password set
        DB:       0,                // use default DB
    })

    // A memolock instance handles multiple resources of the same type,
    // all united by the same tag name, which will be then used as a key
    // prefix in Redis.
    queryResourceTag := "likes"
    queryMemoLock, _ := memolock.NewRedisMemoLock(r, queryResourceTag, 5 * time.Second)
    // This instance has a 5 second default lock timeout:
    // Later in the code you can use the memolock to cache the result of a function and
    // make sure that multiple requests don't cause a stampede.

    // Here I'm requesting a queryset (saved in Redis as a String) and providing  
    // a function that can be used if the value needs to be generated:
    resourceID := "kristoff"
    requestTimeout := 10 * time.Second
    cachedQueryset, _ := queryMemoLock.GetResource(resourceID, requestTimeout, 
        func () (string, time.Duration, error) {
            fmt.Println("Cache miss!\n")
            
            // Sleeping to simulate work. 
            <- time.After(2 * time.Second)

            result := fmt.Sprintf(`{"user":"%s", "likes": ["redis"]}`, resourceID)
            
            // The function will return a value, a cache time-to-live, and an error.
            // If the error is not nil, it will be returned to you by GetResource()
            return result, 5 * time.Second, nil
        },
    )

    fmt.Println(cachedQueryset)
    fmt.Println("Launch the script multiple times, see what changes. Use redis-cli to see what happens in Redis.")
}