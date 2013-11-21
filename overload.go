package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

const VERSION = "0.1.0"

var opts struct {
	NumRequests int      `short:"r" long:"num-requests" description:"Number of requests to make" default:"1"`
	Concurrent  int      `short:"c" long:"concurrent" description:"Number of concurrent connections to make" default:"1"`
	KeepAlive   bool     `short:"k" long:"keep-alive" description:"Use keep alive connection"`
	Headers     []string `short:"H" long:"header" description:"Header to include in request (can be used multiple times)"`
	Version     bool     `long:"version" description:"Display version and exit"`
}

type result struct {
	duration   time.Duration
	statusCode int
	bytesRead  int
	err        error
}

type Summary struct {
	numRequests          int
	totalRequestDuration time.Duration
	avgRequestDuration   time.Duration
	duration             time.Duration
	numSuccesses         int
	numFailures          int
	numUnavailables      int
	requestsPerSecond    float64
}

var requestChan chan *http.Request
var resultChan chan *result
var summaryChan chan *Summary
var client *http.Client

func doRequests() {
	for request := range requestChan {
		startTime := time.Now()
		response, err := client.Do(request)
		if err != nil {
			resultChan <- &result{err: err}
			continue

		}
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			resultChan <- &result{err: err}
			continue
		}

		resultChan <- &result{duration: time.Since(startTime), statusCode: response.StatusCode, bytesRead: len(body)}
	}
}

func generateRequests(target string, headers []string, numRequests int) {
	request, err := http.NewRequest("GET", target, nil)
	if err != nil {
		panic("Bad target")
	}

	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			panic("Header must have key and value separated by ':'")
		}
		request.Header.Add(parts[0], parts[1])
	}

	for i := 0; i < numRequests; i++ {
		requestChan <- request
	}
	close(requestChan)
}

func summarizeResults(numRequests int, startTime time.Time) {
	summary := new(Summary)

	for i := 0; i < numRequests; i++ {
		result := <-resultChan
		summary.numRequests++
		if result.err != nil {
			summary.numUnavailables++
		} else if result.statusCode >= 400 {
			summary.numFailures++
		} else {
			summary.numSuccesses++
			summary.totalRequestDuration += result.duration
		}
	}

	summary.duration = time.Since(startTime)
	if 0 < summary.numSuccesses {
		summary.avgRequestDuration = time.Duration(int64(summary.totalRequestDuration) / int64(summary.numSuccesses))
	}
	summary.requestsPerSecond = float64(summary.numSuccesses) / summary.duration.Seconds()
	summaryChan <- summary
}

func main() {
	var err error
	var args []string

	parser := flags.NewParser(&opts, flags.Default)
	parser.Usage = "[options] URL"
	if args, err = parser.Parse(); err != nil {
		return
	}

	if opts.Version {
		fmt.Println("overload " + VERSION)
		return
	}

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Requires one target URL")
		return
	}

	target := args[0]

	requestChan = make(chan *http.Request)
	resultChan = make(chan *result)
	summaryChan = make(chan *Summary)
	transport := &http.Transport{DisableKeepAlives: !opts.KeepAlive}
	client = &http.Client{Transport: transport}

	startTime := time.Now()

	for i := 0; i < opts.Concurrent; i++ {
		go doRequests()
	}
	go generateRequests(target, opts.Headers, opts.NumRequests)
	go summarizeResults(opts.NumRequests, startTime)

	summary := <-summaryChan

	fmt.Printf("# Requests: %v\n", summary.numRequests)
	fmt.Printf("# Successes: %v\n", summary.numSuccesses)
	fmt.Printf("# Failures: %v\n", summary.numFailures)
	fmt.Printf("# Unavailable: %v\n", summary.numUnavailables)
	fmt.Printf("Duration: %v\n", summary.duration)
	fmt.Printf("Average Request Duration: %v\n", summary.avgRequestDuration)
	fmt.Printf("Requests Per Second: %f\n", summary.requestsPerSecond)
}
