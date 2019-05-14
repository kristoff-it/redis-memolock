# redis-memolock
MemoLock: distributed memoization with promises

# What is a MemoLock?
I chose the name so don't think this is something you should already know.
That said, the name is fairly self-explanatory:

A MemoLock is a form of distributed caching with promises. It's like memoization, but 
since the cache is shared by multiple consumers, each key has a locking mechanism that
ensures that multiple concurrent requests for the same resource don't cause unnecessary
work.

What I just called a *resource* could in fact be considered any serializable input to a
function. This is why I'm equiparating this concept with memoization.
If you don't know what memoziation is, take a look at 
    [the relative wiki article](https://en.wikipedia.org/wiki/Memoization) 
and 
    [the Python standard library's implementation](https://docs.python.org/3/library/functools.html#functools.lru_cache).

The second part of the idea is about notifying immediately (i.e. without polling)
any other request that is waiting for the value to be stored in the cache.

**This means that you can use a MemoLock to handle caching of queries, production of pdf repots,
and really any kind of expensive-but-online computation.**

The implementations in this repository use Redis to cache values and Pub/Sub to resolve
promises across the network.

# Repository Contents
This repository will soon contain a few different implementations that are able to cooperate
(i.e. can generate and resolve promises one from another). While I aim for all implementations
to be good enough to work in production (i.e. no concurrency bugs), the main goal is to write
code that is clear and terse, so that anybody sufficiently motivated can make the right 
adjustments for their own use-cases.

Each implementation has its own README with code examples.

## Go
Inside the `go/` directory you can find a Go module. This implementation makes good use of 
goroutines and channels, and uses a single goroutine to write to the subscription multiplexer,
as opposed to the C# version (not yet available, I will post it in concomitance with 
    [my talk at NDC](https://ndcoslo.com/talk/solving-tricky-coordination-problems-in-stateless-net-services/)
) which has concurrent writers acquire control of a C# `ConcurrentDictionary`.