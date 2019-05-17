# redis-memolock
This is a Go implementation of MemoLock. 

# Installation (skip if using Go modules)
`go get -u github.com/kristoff-it/redis-memolock/go/memolock`

# Documentation
Available at [the usual place](https://godoc.org/github.com/kristoff-it/redis-memolock/go/memolock).

# Launching the example
`go run example/example.go` will launch a HTTP server (default addr: `127.0.0.1:8080`).

Use `curl -get localhost:8080/...` to interact with it. 

The code is fairly self-explanatory.

# Usage
```go
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
    // This instance has a 5 second default lock timeout.
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
    // Launch the script multiple times, see what changes. 
    // Use redis-cli to see what happens in Redis.
}

// MemoLock instances are thread-safe.

```
If you run this program twice within 5 seconds, you will see that "Cache miss!" is 
going to show only once, regardless of whether the first execution has already concluded 
(i.e. the value was cached) or if it's still computing (sleeping instead of doing useful 
work, in the case of this sample code).

# Features
This library also supports:
- Renewing the lock lease: `GetResourceRenewable()`
- Triggering an external application that will report the result via Reids: `GetResourceExternal()`

Read `example/extended_example.go` for more details.


# Is this library production-ready?
Yes, feel free to import this Go module in your code, 
but know that it was created to showcase the possibilities of using Redis Pub/Sub
in a caching scenario. Your needs might be different enough to warrant different
tradeoffs compared to what I chose for this implementation.

Go and Redis make the details of this library extraordinarily clear (and concise, 270LOC).
If you need to make serious use of it, read the code and try to understand where
it might be improved for your specific use-case. A trivial example is the fact that
I'm using Redis Strings to cache results, while you might want a more appropriate data
structure, like a Hash, Set, Sorted Set, Geospatial Index, or even a probabilistic
data structure such as HyperLogLog. 

Checkout https://redis.io to see all the options.