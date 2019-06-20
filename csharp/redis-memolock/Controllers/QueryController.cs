using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Mvc;
using StackExchange.Redis;
using System.Collections.Concurrent;
using Microsoft.AspNetCore.Session;

namespace redis_memolock.Controllers
{
    [Route("[controller]")]
    [ApiController]
    public class QueryController : ControllerBase
    {
        private static ConnectionMultiplexer redis = ConnectionMultiplexer.Connect("localhost");
        private static RedisMemoLock reportLock = new RedisMemoLock(redis, "query");
        //  query:foo
        //  query/lock:foo
        //  query/notif:foo

        // GET query/simple
        [HttpGet("simple/{id}")]
        public async Task<ActionResult<string>> GetSimpleAsync(string id)
        {
            var requestTimeout = TimeSpan.FromSeconds(5);
            var reportQuerySet = await reportLock.GetResource(id, requestTimeout, async () => 
            {
                Console.WriteLine($"(query/simple/{id}) Working hard!");
                
                // Simulate some hard work like fecthing data from a DBMS
                await Task.Delay(TimeSpan.FromSeconds(3));

                return ($"<query set result for {id}>", TimeSpan.FromSeconds(10));
            });

            return reportQuerySet;
        }
    }
}
