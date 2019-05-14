package main

import (
	"fmt"
	"net/http"
	"time"
	"github.com/go-redis/redis"
	"github.com/kristoff-it/redis-memolock/go/memolock"
	"github.com/gorilla/mux"
	"os/exec"
)

// Address for the HTTP Server to bind to.
const BindAddr = "127.0.0.1:8080"

func main() {
	r := redis.NewClient(&redis.Options{
    	Addr:     "localhost:6379", // use default Addr
    	Password: "",               // no password set
    	DB:       0,                // use default DB
	})
	router := mux.NewRouter()
	srv := &http.Server{
        Handler: router,
        Addr:    BindAddr,
    }

	//  query:foo
	//  query/lock:foo
	//  query/notif:foo
	queryMemolock, _ := memolock.NewRedisMemoLock(r, "query", 5 * time.Second)

	// GET query/simple
	router.HandleFunc("/query/simple/{id}", func (w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"] // extract {id} from the url path
		
		requestTimeout := 10 * time.Second
		cachedQueryset, _ := queryMemolock.GetResource(id, requestTimeout, func () (string, time.Duration, error) {
			fmt.Printf("(query/queryset/%s) Working hard!\n", id)

			// Simulate some hard work like fecthing data from a DBMS
			<- time.After(2 * time.Second)
			result := fmt.Sprintf("<query set result %s>", id)

			return result, 5 * time.Second, nil
		})

		fmt.Fprint(w, cachedQueryset)
	})

	//  report:foo
	//  report/lock:foo
	//  report/notif:foo
	reportMemolock, _ := memolock.NewRedisMemoLock(r, "report", 5 * time.Second)

	// GET report/renewable
	router.HandleFunc("/report/renewable/{id}", func (w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"] // extract {id} from the url path
		
		requestTimeout := 10 * time.Second
		reportPdfLocation, _ := reportMemolock.GetResourceRenewable(id, requestTimeout, func (renew memolock.LockRenewFunc) (string, time.Duration, error) {
			fmt.Printf("(report/renewable/%s) Working super-hard! (1)\n", id)
			<- time.After(2 * time.Second)

            // It turns out we have to do a lot of work, renew the lock!
			_ = renew(20 * time.Second)

			// Simulate some hard work
			<- time.After(6 * time.Second)
			fmt.Printf("(report/renewable/%s) Working super-hard! (2)\n", id)
			result := fmt.Sprintf("https://somewhere/%s-report.pdf", id)

			return result, 5 * time.Second, nil
		})

		fmt.Fprint(w, reportPdfLocation)
	}) 



	// GET report/oh-no
	router.HandleFunc("/report/oh-no/{id}", func (w http.ResponseWriter, r *http.Request) {
		id := mux.Vars(r)["id"] // extract {id} from the url path
		
		requestTimeout := 10 * time.Second
		reportPdfLocation, err := reportMemolock.GetResourceRenewable(id, requestTimeout, func (renew memolock.LockRenewFunc) (string, time.Duration, error) {
			fmt.Printf("(report/oh-no/%s) Working super-hard! (1)\n", id)
			<- time.After(6 * time.Second)

            // It turns out we have to do a lot of work, renew the lock!
			// Oh no, we are already out of time!
			err := renew(20 * time.Second)
			if err != nil {
				return "", 0, err
			}

			// Simulate some hard work
			<- time.After(6 * time.Second)
			fmt.Printf("(report/renewable/%s) Working super-hard! (2)\n", id)
			result := fmt.Sprintf("https://somewhere/%s-report.pdf", id)

			return result, 50 * time.Second, nil
		})

		if err != nil {
			fmt.Fprintf(w, "ERROR: %v \n", err)
			return
		}

		fmt.Fprint(w, reportPdfLocation)
	})

	//  ext:foo
	//  ext/lock:foo
	//  ext/notif:foo
	extMemolock, err := memolock.NewRedisMemoLock(r, "ext", 15 * time.Second)
	if err != nil {
		panic(err)
	}
	// GET ext/stemmer/forniture
	router.HandleFunc("/ext/stemmer/{word}", func (w http.ResponseWriter, r *http.Request) {
		word := mux.Vars(r)["word"] // extract {word} from the url path
		
		requestTimeout := 10 * time.Second
		stemming, _ := extMemolock.GetResourceExternal(word, requestTimeout, func () error {
			fmt.Printf("(ext/stemmer/%s) Working hard!\n", word)
			
			// We don't .Output() / try to read stdout, because we will be notified from Redis.
			go exec.Command("python", "python_service/stemmer.py", word).Run()
			
			return nil
		})

		fmt.Fprint(w, stemming)
	})

	fmt.Println("Listening to ", BindAddr)
	fmt.Println("GET /query/simple/{foo}     -> simple caching")
	fmt.Println("GET /report/renewable/{foo} -> renewable lock ")
	fmt.Println("GET /report/oh-no/{bar}     -> failure case for renewable lock")
	fmt.Println("GET /ext/stemmer/{banana}   -> external program interop")

	srv.ListenAndServe()
}

