# redis-memolock
This is a Go implementation of MemoLock. 

# Installation (skip if using go modules)
`go get -u github.com/kristoff-it/redis-memolock/go/memolock`

# Launching the example
`go run example/example.go` will launch a HTTP server (default addr: `127.0.0.1:8080`).
Use `curl -get localhost:8080/...` to interact with it. The code is fairly self-explanatory.

# Usage
```go
import "github.com/kristoff-it/redis-memolock/go/memolock"

// A memolock instance handles multiple resources of the same type,
// all accomunated by the same tag name, which will be then used as
// a key prefix in Redis.
queryResourceTag := "query-set"

// This instance has a 5 second default lock timeout.
queryMemolock, _ := memoLock.NewRedisMemoLock(r, queryResourceTag, 5 * time.Second)
// Later in the code you can use the memolock to cache the result of a function and
// make sure that multiple requests don't cause a stampede.

// Here I'm requesting a queryset (saved in Redis as a String) and providing a function
// that can be used if the value needs to be generated.
resourceID := "user-kristoff-recommendations"
requestTimeout := 10 * time.Second
cachedQueryset, _ := queryMemoLock.GetResource(resourceID, requestTimeout, func () (string, time.Duration, error) {
    result := fmt.Sprintf("<query set result %s>", resourceID)
    
    // The function will return the value, the cache time-to-live, and an error.
    // If the error is not nil, it will be returned to you by GetResource()
    return result, 5 * time.Second, nil
})

// MemoLock instances can

// The library also supports: 
// - renewing the lock lease
// - using the anonymous function to launch an external program that will notify completion through Redis
// Read example/example.go for more details.
```


# Is this library production-ready?
It works well enough, so feel free to import this Go module in your code, 
but know that it was created to showcase the possibilities of using Redis Pub/Sub
in a caching scenario. Your needs might be different enough to warrant different
tradeoffs compared to what I chose for this implementation.

Go and Redis make the details of this library extraordinarily clear.
If you need to make serious use of it, read the code and try to understand where
it might be improved for your specific use-case. A trivial example could be that
I'm using Redis Strings to cache results. You might want a more appropriate data
structure, like a Hash, Set, Sorted Set, Geospatial Index, or even a probabilistic
data structure such as HyperLogLog.