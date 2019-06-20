using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using StackExchange.Redis;
using System.Collections.Concurrent;
using System.Linq;

namespace redis_memolock
{

    public class RedisMemoLock
    {
        public class ExtensionFailedException : Exception {};
        public class InvalidResourceTokenException : Exception {};

        private ConnectionMultiplexer _redis;
        private ConcurrentDictionary<string, IEnumerable<TaskCompletionSource<string>>> _waitDict = new ConcurrentDictionary<string, IEnumerable<TaskCompletionSource<string>>>();
        private string _resourceTag;
        private TimeSpan _lockTimeout = TimeSpan.FromSeconds(60);
        private static LuaScript _lockScript = LuaScript.Prepare(
            // Lua script to atomically renew the lock lease if and only if
            // the lock is till owned by the same request.
            @"
                if redis.call('GET', @Key) == @CurrentRequest
                then 
                    redis.call('EXPIRE', @Key, @Seconds) 
                    return 1
                else 
                    return 0
                end
            "
        );

        public RedisMemoLock(ConnectionMultiplexer redis, string resourceTag, TimeSpan lockTimeout = default(TimeSpan))
        {
            _redis = redis;
            _resourceTag = resourceTag;
            if (lockTimeout != default(TimeSpan)) 
            {
                _lockTimeout = lockTimeout;
            }   

            var sub = redis.GetSubscriber();
            sub.Subscribe($"{_resourceTag}/notif:*", handleNotification);
        }

        private void handleNotification(RedisChannel channel, RedisValue message)
        {
            // Pop from the ConcurrentDict all waiters for the given notification.
            IEnumerable<TaskCompletionSource<string>> waitList = null;
            if (_waitDict.TryRemove(channel, out waitList)) 
            {
                foreach (var task in waitList) 
                {
                    // Resolve the task.
                    task.TrySetResult((string) message);
                }
            }
        }

        #region Public API

        public async Task<string> GetResource(string id, TimeSpan waitTimeout, Func<Task<(string, TimeSpan?)>> generatingFunc)
        {
            
            return await _getResource(id, generatingFunc, Guid.NewGuid().ToString(), waitTimeout, false);
        }

        public async Task<string> GetResource(string id, TimeSpan waitTimeout, Func<Func<TimeSpan,Task>,Task<(string, TimeSpan?)>> generatingFunc)
        {
            var db = _redis.GetDatabase();
            var requestId =  Guid.NewGuid().ToString();

            // We give an async function to the client's generatingFunc that will extend the lock by the given TimeSpan.
            // The function that we pass to the client's generatingFunc throws if the extension failed.
            Func<TimeSpan,Task> renew = async (extension) => 
            {
                if (0 == (int) await db.ScriptEvaluateAsync(_lockScript, new { Key = (RedisKey) $"{_resourceTag}/lock:{id}", CurrentRequest = requestId, Seconds = extension.TotalSeconds}))
                {
                    throw new ExtensionFailedException();
                }
            };
            return await _getResource(id, async () => await generatingFunc(renew), requestId, waitTimeout, false);
        }

        public async Task<string> GetExternallyManagedResource(string id, TimeSpan waitTimeout, Func<Task> generatingFunc)
        {
            return await _getResource(id, async () => { await generatingFunc(); return ("", null);}, Guid.NewGuid().ToString(), waitTimeout, true);
        }

        #endregion

        # region Private API
        private async Task<string> _getResource(string id, Func<Task<(string, TimeSpan?)>> generatingFunc, string requestId, TimeSpan waitTimeout, bool externallyManaged)
        {

            // If the resource is available, return it immediately.
            var db = _redis.GetDatabase();
            var resourceValue = (string) await db.StringGetAsync($"{_resourceTag}:{id}");
            if (resourceValue != null) 
            {
                return resourceValue;
            }
            
            // The resource is not available, can we get the lock?
            var resourceLock = await db.StringSetAsync($"{_resourceTag}/lock:{id}", requestId, _lockTimeout, When.NotExists);
            if (resourceLock)
            {
                // We acquired the lock, use the client-provided func to generate the resource.
                TimeSpan? resourceTTL;
                (resourceValue, resourceTTL) = await generatingFunc();

                if (resourceValue == null)
                {
                    // null is not a valid value for Redis Strings
                    resourceValue = "";
                }

                if (!externallyManaged) 
                {
                    // Storage of the value on Redis and notification is handled
                    // by us and we can return the value immediately.
                    await Task.WhenAll(
                        // This syntax enables pipelining of commands.
                        db.StringSetAsync($"{_resourceTag}:{id}", resourceValue, resourceTTL),
                        db.PublishAsync($"{_resourceTag}/notif:{id}", resourceValue)
                    );
                    return resourceValue;
                }

                // The notification will be created by an external system 
                // so we falltrough and subscribe to notifs anyway.
            } 
            
            // The resource is not ready yet so we wait for a notification of completion.
            var timeoutTask = Task.Delay(waitTimeout == default(TimeSpan) ? _lockTimeout : waitTimeout);
            var notifTask = setupNotification(id);
            var firstResult = await Task.WhenAny(notifTask, timeoutTask);
            if (firstResult == timeoutTask)
            {
                // TODO: remove promise from dict 
                throw new TimeoutException("the operation timed out");
            }

            // We did not timeout, let's return the value.
            return await (firstResult as Task<string>);

        }

        private async Task<string> setupNotification(string id) 
        {
            var manuallyResolved = new TaskCompletionSource<string>();
            
            // First subscribe to the notification
            {                
                var waitList = new List<TaskCompletionSource<string>>{manuallyResolved}; 
                
                // Atomic update
                _waitDict.AddOrUpdate($"{_resourceTag}/notif:{id}", waitList, (key, oldList) => oldList.Concat(waitList));
            }


            // Then check again if the key is not present, because we might have just missed the notification.
            {
                var db = _redis.GetDatabase();
                var resourceLocation = (string) await db.StringGetAsync($"{_resourceTag}:{id}");
                if (resourceLocation != null) 
                {
                    // We artificially invoke the notification handling function to also perform cleanup.
                    handleNotification(id, resourceLocation);
                }
            }

            return await manuallyResolved.Task;
        }
        #endregion
    }
}
