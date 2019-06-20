using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Mvc;
using StackExchange.Redis;
using System.Collections.Concurrent;

namespace redis_memolock.Controllers
{
    [Route("[controller]")]
    [ApiController]
    public class ReportController : ControllerBase
    {
        private static ConnectionMultiplexer redis = ConnectionMultiplexer.Connect("localhost");
        private static RedisMemoLock reportLock = new RedisMemoLock(redis, "report");
        //  report:foo
        //  report/lock:foo
        //  report/notif:foo

        // GET report/renewable
        [HttpGet("renewable/{id}")]
        public async Task<ActionResult<string>> GetRenewableAsync(string id)
        {
            var requestTimeout = TimeSpan.FromSeconds(5);
            var reportPdfLocation = await reportLock.GetResource(id, requestTimeout, async (renew) => 
            {

                Console.WriteLine($"(report/renewable/{id}) Working super-hard! (1)");
                await Task.Delay(TimeSpan.FromSeconds(2));

                // It turns out we have to do a lot of work, renew the lock!
                await renew(TimeSpan.FromSeconds(20));

                // Simulate some hard work
                await Task.Delay(TimeSpan.FromSeconds(6));
                Console.WriteLine($"(report/renewable/{id}) Working super-hard! (2)");

                return ($"https://somewhere/{id}-report.pdf", TimeSpan.FromSeconds(10));
            });

            return reportPdfLocation;
        }

        // GET report/oh-no
        [HttpGet("oh-no/{id}")]
        public async Task<ActionResult<string>> GetOhNoAsync(string id)
        {
            var requestTimeout = TimeSpan.FromSeconds(5);
            var reportPdfLocation = await reportLock.GetResource(id, requestTimeout, async (renew) => 
            {

                // Simulate some hard work
                Console.WriteLine($"(report/oh-no/{id}) Working super-hard! (1)");
                await Task.Delay(TimeSpan.FromSeconds(6));

                // It turns out we have to do a lot of work, renew the lock!
                // Oh no, we are already out of time, what will happen?
                await renew(TimeSpan.FromSeconds(20));

                Console.WriteLine($"(report/oh-no/{id}) Working super-hard! (2)");

                return ($"https://somewhere-else/{id}-report.pdf", TimeSpan.FromSeconds(10));
            });

            return reportPdfLocation;
        }
    }
}
