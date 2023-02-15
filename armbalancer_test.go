package armbalancer

import (
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestSoak(t *testing.T) {
	limit := 20

	reqCountByAddr := map[string]int{}
	var lock sync.Mutex
	var totalRequests int
	svr := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()

		if r.Proto != "HTTP/2.0" {
			t.Errorf("received request with proto: %s", r.Proto)
		}

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
	var closed int
	svr.Config.ConnState = func(c net.Conn, cs http.ConnState) {
		if cs == http.StateClosed {
			closed++
		}
	}
	svr.EnableHTTP2 = true
	svr.StartTLS()
	defer svr.Close()

	u, _ := url.Parse(svr.URL)
	client := &http.Client{Transport: New(Options{
		Transport:            svr.Client().Transport.(*http.Transport),
		Host:                 u.Host,
		PoolSize:             8,
		RecycleThreshold:     5,
		MinReqsBeforeRecycle: 6,
	})}

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
					continue
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

	if closed < len(reqCountByAddr)/4 {
		t.Errorf("expected at least 25 percent of connections to be closed but only %d were closed", closed)
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
	thres := len(reqCountByAddr) / 10
	if l := len(overLimit); l > thres {
		t.Errorf("%d clients exceeded the rate limit: %+s", l, overLimit)
	}
	if l := len(underMin); l > thres {
		t.Errorf("%d clients undershot the configured min requests per connection: %+s", l, underMin)
	}
}

func TestHooks(t *testing.T) {
	svr := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ms-Ratelimit-Remaining-Test", "1")
	}))
	svr.EnableHTTP2 = true
	svr.StartTLS()
	defer svr.Close()

	var lock sync.Mutex
	recycleHookCalls := 0
	u, _ := url.Parse(svr.URL)
	client := &http.Client{Transport: New(Options{
		Transport:            svr.Client().Transport.(*http.Transport),
		Host:                 u.Host,
		PoolSize:             8,
		RecycleThreshold:     5,
		MinReqsBeforeRecycle: 2,
		OnConnRecycle: func(connID int, requestCount int64, connAge, drainLatency time.Duration) {
			lock.Lock()
			defer lock.Unlock()

			recycleHookCalls++
			if requestCount != 2 {
				t.Logf("unexpected request count: %d", requestCount)
			}

			t.Logf("OnConnRecycle: connID=%d requestCount=%d connAge=%s drainLatency=%s", connID, requestCount, connAge, drainLatency)
		},
	})}

	for i := 0; i < 10; i++ {
		req, _ := http.NewRequest("GET", svr.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Error(err)
			continue
		}
		resp.Body.Close()
	}

	lock.Lock()
	defer lock.Unlock()
	if recycleHookCalls == 0 {
		t.Error("OnConnRecycle was not called")
	}
}
