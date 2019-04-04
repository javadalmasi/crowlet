package crawler

import (
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/tcnksm/go-httpstat"
)

// HTTPResponse holds information from a GET to a specific URL
type HTTPResponse struct {
	URL      string
	Response *http.Response
	Result   httpstat.Result
	EndTime  time.Time
	Err      error
}

// HTTPConfig hold settings used to get pages via HTTP/S
type HTTPConfig struct {
	User string
	Pass string
}

// HTTPGet issues a GET request to a single URL and returns an HTTPResponse
func HTTPGet(url string, config HTTPConfig) (response *HTTPResponse) {
	response = &HTTPResponse{
		URL: url,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Error(err)
		response.Err = err
		return
	}

	// create a httpstat powered context
	var result httpstat.Result
	ctx := httpstat.WithHTTPStat(req.Context(), &result)
	req = req.WithContext(ctx)

	// set http basic if provided
	if len(config.User) > 0 {
		req.SetBasicAuth(config.User, config.Pass)
	}

	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err)
		response.Err = err
		return
	}

	// Explicitly Drain & close the body to allow faster
	// reuse of the transport
	defer func() {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()

	end := time.Now()
	total := int(result.Total(end).Round(time.Millisecond) / time.Millisecond)

	response.EndTime = end
	response.Response = resp
	response.Result = result

	if log.GetLevel() == log.DebugLevel {
		log.WithFields(log.Fields{
			"status":  resp.StatusCode,
			"dns":     int(result.DNSLookup / time.Millisecond),
			"tcpconn": int(result.TCPConnection / time.Millisecond),
			"tls":     int(result.TLSHandshake / time.Millisecond),
			"server":  int(result.ServerProcessing / time.Millisecond),
			"content": int(result.ContentTransfer(end) / time.Millisecond),
			"time":    total,
			"close":   end,
		}).Debug("url=" + url)
	} else {
		log.WithFields(log.Fields{
			"status":     resp.StatusCode,
			"total-time": total,
		}).Info("url=" + url)
	}

	return
}

// ConcurrentHTTPGets will GET the urls passed and result the results of the crawling
func ConcurrentHTTPGets(urls []string, config HTTPConfig, maxConcurrent int,
	quit chan struct{}) <-chan *HTTPResponse {
	resultChan := make(chan *HTTPResponse, len(urls))
	httpResources := make(chan int, maxConcurrent-1)

	go func() {
		var wg sync.WaitGroup

		defer func() {
			wg.Wait()
			close(resultChan)
		}()

		for _, url := range urls {
			select {
			case <-quit:
				log.Info("Waiting for workers to finish...")
				return
			case httpResources <- 1:
				wg.Add(1)

				go func(url string) {
					defer func() {
						<-httpResources
						wg.Done()
					}()

					resultChan <- HTTPGet(url, config)
				}(url)
			}
		}
	}()

	return resultChan
}