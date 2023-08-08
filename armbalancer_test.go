package armbalancer

import (
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
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
	if err == nil || strings.Contains(err.Error(), `Get "http://not-the-host": host "not-the-host" is not supported by the configured ARM balancer, supported host name is %q`) {
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

type testCase struct {
	name      string
	reqHost   string
	transHost string
	transPort string
	expected  bool
}

func TestCompareHost(t *testing.T) {
	cases := []testCase{
		{
			name:      "matched since all without port number",
			reqHost:   "host.com",
			transHost: "host.com",
			transPort: "443",
			expected:  false,
		},
		{
			name:      "matched since all with port number",
			reqHost:   "host.com:443",
			transHost: "host.com",
			transPort: "443",
			expected:  true,
		},
		{
			name:      "matched with appending port name",
			reqHost:   "host.com:443",
			transHost: "host.com",
			transPort: "443",
			expected:  true,
		},
		{
			name:      "not matched since different port number",
			reqHost:   "host.com:443",
			transHost: "host.com",
			transPort: "11254",
			expected:  false,
		},
		{
			name:      "not matched since differnt host name without port number",
			reqHost:   "host.com",
			transHost: "abc.com",
			expected:  false,
		},
		{
			name:      "not matched since differnt host name with port number",
			reqHost:   "host.com:443",
			transHost: "abc.com",
			transPort: "443",
			expected:  false,
		},
		{
			name:      "not matched since differnt host name with port number for reqHost only",
			reqHost:   "host.com:443",
			transHost: "abc.com",
			transPort: "443",
			expected:  false,
		},
		{
			name:      "not matched since differnt host name with port number for transHost only",
			reqHost:   "host.com",
			transHost: "abc.com",
			transPort: "443",
			expected:  false,
		},
	}

	for index, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := hostScopedTransport{
				pool: map[string]*transportPool{
					c.transHost + ":" + c.transPort: &transportPool{pool: []http.RoundTripper{http.DefaultTransport}},
				},
			}
			_, err := r.compareHosts(&url.URL{Host: c.reqHost})
			if (err != nil) == c.expected {
				t.Errorf("expected %d result \"%t\" is not same as we get: %s", index, c.expected, err.Error())
			}
		})
	}
}

func TestNew(t *testing.T) {
	type args struct {
		opts Options
	}
	tests := []struct {
		name     string
		args     args
		wantHost string
		wantPort string
		paniced  bool
	}{
		{
			name: "invalid host",
			args: args{
				opts: Options{
					Host: "invalid:host:invalidport",
				},
			},
			paniced: true,
		},
		{
			name: "host is not assigned",
			args: args{
				opts: Options{
					Host: ":445",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "445",
			paniced:  false,
		},
		{
			name: "port is not assigned",
			args: args{
				opts: Options{
					Host: "management.azure.com",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "443",
			paniced:  false,
		},
		{
			name: "hosturl is not assigned",
			args: args{
				opts: Options{
					Host: "",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "443",
			paniced:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.paniced {
				defer func() {
					if r := recover(); r != nil {
						return
					}
					t.Errorf("New() did not panic")
				}()
			} else {
				tt.args.opts.TransportFactory = map[string]Transport{
					strings.ToLower(tt.wantHost + ":" + tt.wantPort): func(id int, parent *http.Transport, host string, port string, recycleThreshold, minReqsBeforeRecycle int64) http.RoundTripper {
						if host != tt.wantHost {
							t.Errorf("New() host = %v, want %v", host, tt.wantHost)
						}
						if port != tt.wantPort {
							t.Errorf("New() port = %v, want %v", port, tt.wantPort)
						}
						return nil
					},
				}
			}
			if got := New(tt.args.opts); got == nil {
				t.Errorf("New() returned nil")
			}
		})
	}
}
