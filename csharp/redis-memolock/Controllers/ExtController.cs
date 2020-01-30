using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Mvc;
using StackExchange.Redis;
using System.Collections.Concurrent;
using System.Diagnostics;

namespace redis_memolock.Controllers
{
    [Route("[controller]")]
    [ApiController]
    public class ExtController : ControllerBase
    {
        private static ConnectionMultiplexer redis = ConnectionMultiplexer.Connect("localhost");
        private static RedisMemoLock aiLock = new RedisMemoLock(redis, "ext");
        //  ext:foo
        //  ext/lock:foo
        //  ext/notif:foo

        // GET ext/stemmer
        [HttpGet("stemmer/{word}")]
        public async Task<ActionResult<string>> GetExternal1Async(string word)
        {
            var requestTimeout = TimeSpan.FromSeconds(10);
            var result = await aiLock.GetExternallyManagedResource(word, 
                requestTimeout, async () => 
            {
                Console.WriteLine($"(ext/stemmer/{word}) Working hard!");
                await Task.Delay(TimeSpan.FromSeconds(1));

                var py = new ProcessStartInfo {
                    FileName = "/usr/bin/env",
                    Arguments = $"python3 python_service/stemmer.py {word}",
                    UseShellExecute = false,
                    CreateNoWindow = true
                };
                
                Process.Start(py); // We don't .WaitForExit() / try to read stdout, because we will be notified from Redis.
            });

            return result;
        }

        // GET ext/joke
        [HttpGet("joke")]
        public async Task<ActionResult<string>> GetExternal2Async()
        {
            #region JokeCode
            var resource = "joke";
            var computeTask = aiLock.GetExternallyManagedResource(resource, 
                TimeSpan.FromSeconds(70), async () => 
            {
                // NatureScript (TM) code
                var natureScript = new string[]
                {
                    "GREETINGS HUMANS",
                    "IT IS I",
                    "* THE GREAT .NET CORE RUNTIME *",
                    "",
                    "A TASK IS BESTOWED UPON YOU",
                    "DETERMINE THE FOLLOWING: ",
                    "",
                    "'HOW MUCH WOOD WOULD A WOODCHUCK CHUCK",
                    "IF A WOODCHUCK COULD CHUCK WOOD?'",
                    "",
                    "PROVIDE THE CORRECT ANSWER IN GRAMS",
                    "",
                    "YOU MAY NOW PROCEED",
                    "",
                    "",
                };

                foreach (var line in natureScript) 
                {
                    Console.WriteLine(line);
                    await Task.Delay(TimeSpan.FromSeconds(1));
                } 
            });
            

            // Timeout while waiting to print more lines
            var pingHumans = new string[]
            {
                "HUMANS I HOPE YOU ARE WORKING HARD",
                "THE CLOCK IS TICKING",
                "HOW LONG CAN IT TAKE",
                "IS A SIMPLE QUESTION ON AGENT BEHAVIOR",
                "THERE IS NO MUCH TIME LEFT",
                "PRETTY PLEASE? :("
            };

            var i = 0;
            while(computeTask != await Task.WhenAny(computeTask, Task.Delay(TimeSpan.FromSeconds(i == 0 ? 20 : 7))))
            {
                if (i < pingHumans.Length) 
                {
                    Console.WriteLine(pingHumans[i]);
                    i++;
                }
            }
            
            string result;
            try 
            {
                result = await computeTask;
            }
            catch(TimeoutException)
            {
                Console.WriteLine("\n\nNOOOOOO - ALL IS LOST");
                Console.WriteLine("arghh... my segmentation ...faults");
                return null;
            }
            
            if (i > 0)
            {
                Console.WriteLine($"\n\nYOU REPORTED `{result}` AS RESULT");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("I TRUST YOUR ANSWER IS CORRECT");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("FOR I AM THE MIGHTY .NET CORE RUNTIME");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("AND I DO NOT INTEND TO CHECK.");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("");
                await Task.Delay(TimeSpan.FromSeconds(1));
                Console.WriteLine("Thanks for listening!\nYou can find me on Twitter @croloris");
            }

            return result;
            #endregion
        }
    }
}
