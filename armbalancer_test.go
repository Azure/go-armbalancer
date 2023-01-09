package armbalancer

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

func TestSoak(t *testing.T) {
	limit := 20

	reqCountByAddr := map[string]int{}
	var lock sync.Mutex
	var totalRequests int
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()

		if r.Header.Get("Test") != "true" {
			return // don't handle any requests from outside the test
		}

		if _, ok := reqCountByAddr[r.RemoteAddr]; !ok && rand.Intn(100) == 1 {
			// randomly start new connections with zero quota to test min reqs per connection configuration
			reqCountByAddr[r.RemoteAddr] = limit
		} else {
			reqCountByAddr[r.RemoteAddr]++
		}

		totalRequests++
		w.Header().Set("X-Ms-Ratelimit-Remaining-Test", strconv.Itoa(limit-reqCountByAddr[r.RemoteAddr]))
		w.Header().Set("X-Ms-Ratelimit-Remaining-Dummy", "10")
		w.Header().Set("X-Ms-Ratelimit-Remaining-Invalid", "not-a-number")
	}))
	defer svr.Close()

	u, _ := url.Parse(svr.URL)
	client := &http.Client{Transport: New(http.DefaultTransport.(*http.Transport), u.Host, 8, 5, 6)}

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Add(-1)
			for j := 0; j < 500; j++ {
				req, _ := http.NewRequest("GET", svr.URL, nil)
				req.Header.Set("Test", "true")
				resp, err := client.Do(req)
				if err != nil {
					t.Error(err)
				}
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	_, err := client.Get("http://not-the-host")
	if err == nil || err.Error() != `Get "http://not-the-host": host "not-the-host" is not supported by the configured ARM balancer` {
		t.Errorf("expected error when requesting host other than the one configured, got: %s", err)
	}

	if l := len(reqCountByAddr); l < 100 {
		t.Errorf("pool couldn't be working correctly as only %d connections to the server were created", l)
	}

	overLimit := []string{}
	underMin := []string{}
	for addr, count := range reqCountByAddr {
		if count > limit {
			overLimit = append(overLimit, addr)
		}
		if count < 6 {
			underMin = append(underMin, addr)
		}
	}

	// Since connection recycling is async, we can't expect 100% conformance to the configured limits
	thres := len(reqCountByAddr) / 4 // 25%
	if l := len(overLimit); l > thres {
		t.Errorf("%d clients exceeded the rate limit: %+s", l, overLimit)
	}
	if l := len(underMin); l > thres {
		t.Errorf("%d clients undershot the configured min requests per connection: %+s", l, underMin)
	}
}
