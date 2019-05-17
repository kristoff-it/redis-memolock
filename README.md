# Redis MemoLock
Redis MemoLock - Distributed Memoization with Promises 

## What is a MemoLock?
I chose the name so don't think this is something you should already know.
That said, the name is fairly self-explanatory:

**A MemoLock is a form of distributed caching with promises. It's like memoization, but 
since the cache is shared by multiple consumers, each key has a locking mechanism that
ensures that multiple concurrent requests for the same resource don't cause unnecessary
work.**

While I claim to have come up with the name, the concept is not new (as always), 
    [here](https://instagram-engineering.com/thundering-herds-promises-82191c8af57d) 
you can read about Instagram having a similar concept in their architecture (but I did submit 
    [a talk to NDC on the same subject](https://ndcoslo.com/talk/solving-tricky-coordination-problems-in-stateless-net-services/) 
before their post, so I claim it was an indepentent discovery, ha!).

What I just called a *resource* could in fact be considered any serializable input to a
function. This is why I'm equiparating this concept with memoization.
If you don't know what memoziation is, take a look at 
    [the relative wiki article](https://en.wikipedia.org/wiki/Memoization) 
and 
    [the Python standard library's implementation](https://docs.python.org/3/library/functools.html#functools.lru_cache).

The second part of the idea is about notifying immediately (i.e. without polling)
any other request that is waiting for the value to be available in the cache.

**This means that you can use a MemoLock to handle caching of queries, production of pdf reports,
and really any kind of expensive-but-online computation.**

The implementations in this repository use Redis to cache values and Pub/Sub to resolve
promises across the network. Since Redis can be replicated and clustered, you can take this 
library up to any scale.

## Features 

### Polyglot
It works across different languages A client only needs a Redis client library and 
knowledge of the key naming scheme in use, and is able to generate/resolve promises with any other.

### Scalable
No polling or other wasteful patterns, and it can scale efficiently in a clustered deployment.\
This is something that Redis is in a unique position to provide.

### Flexible, Without Foot Guns
It tries to ensure that useless work doesn't happen but, being part of a distributed system, 
there is no strong guarantee, as it would necessarily require much more coordination and, consequently, 
lead to lower scalability.\
It tries to get a good tradeoff in that regard.

## How does it work?
1. As a service instance, when we need to fetch `likes` for `kristoff` (i.e. `likes:kristoff`), we look for it in Redis.
    If it's there, we're done.
2. If the key is not present, we try to acquire `likes/lock:kristoff` using SET with NX.
    The NX option will ensure that in case of concurrent requests, only one will be able to set the key succesfully.
3. If we are able to acquire the lock, it means that it's our job to generate the value (e.g. fetch it from DB).
    Once we're done, we save it to Redis and send a message on a Pub/Sub channel called `likes/notif:kristoff` 
    to notify all other potentially awaiting clients that the value is now available.
4. If we were **not** able to acquire the lock, we just subscribe to `likes/notif:kristoff`.
    The service that succeeded in locking the resource to notify us that the value is now available 
    *(as described in the previous step)*.

This is a high level description of what redis-memolock does for you.

In practice, to get the concurrency right, there are a few more branches involved, but it has no impact
on the public interface, so you only have to care about generating the content and handling time-outs.

# Repository Contents
This repository will soon contain a few different implementations that are able to cooperate
(i.e. can generate and resolve promises one from another). While I aim for all implementations
to be good enough to work in production (i.e. no concurrency bugs), the main goal is to write
code that is clear and terse, so that anybody sufficiently motivated can make the right 
adjustments for their own use-cases.

Each implementation has its own README with code examples.

### C#
Coming in concomitance with [my talk at NDC](https://ndcoslo.com/talk/solving-tricky-coordination-problems-in-stateless-net-services/).

### Go
[See `go/README.md`](go/).

Inside the `go/` directory you can find a Go module. This implementation makes good use of 
goroutines and channels, and uses a single goroutine to write to the subscription multiplexer,
as opposed to the C# version which has concurrent writers acquire control of a `ConcurrentDictionary`.


## !! WARNING !!
This library is all about nimble locking for enhancing performance. It's ok to use it in combination
with external systems (e.g. store the result of the computation elsewhere, like a CDN if it's a PDF
report, and just save in Redis a token representing the location) but it's **NOT** ok to use it to 
lock computations that rely on mutual exclusion for correctness. **This locking mechanism is about 
doing less work, not correct work.** 

A **good example** is locking database reads: two reads at the same time won't cause any problem
and the last writer will win.

A **not-so-good example** is trying to upload a file to an FTP server (or CDN) with a non-unique name: 
what happens if two writers try to write to the same filename?\
*Answer: in reasonable implementations one writer will fail and report an error.*\
*Fix: make sure filenames generated by different writers can't collide (e.g. use UUIDs), or catch the 
error if you can distinguish it from other types of error (i.e. you get a FileAlreadyExistsError, and 
not a GenericOpenError).*

A **bad example** is using a MemoLock to guard a computation that might be corrupted by concurrent
writers. If your mistake is bad enough, you might end up in a situation where both writes succeed
and the result becomes corrupted. Don't use this lock to do distributed transactions, for example.\
*Fix: just don't.*

I'm writing this warning because distributed locking is a complex subject and it's easy to misuse
tools if you expect from them greater guarantees than they actually provide. As stated previously,
this library tries to be lightweight to enhance performance, not guarantee full mutual exclusion.
While not providing such functionality can be seen as a limitation, the upside is that such library
would not be able scale as much (because of a higher level of coordination) and would not allow you
to use services that are not Redis-aware to store results, such as a CDN, for example.

*Enjoy the simplicity and flexibility that springs from limiting the scope of our design.*

## How can different implementations share promises?
Here the term *promise* is used in a fairly abstract way with only a small connection to any specific language implementation.
Different implementations can interoperate because they share a Redis client and the understanding of three concepts:

1. Keys are stored using the scheme `<resource tag>:<resource id>`\
   (e.g. `likes:kristoff`)
2. Locks are stored using the scheme`<resource tag>/lock:<resource id>`\
   (e.g. `likes/lock:kristoff`)
3. Pub/Sub notifications are sent over the channel `<resource tag>/notif:<resource id>`\
   (e.g. `likes/notif:kristoff`)

Any client that can `SET` and `GET` a key, and that can use Pub/Sub, can interoperate transparently with all others.