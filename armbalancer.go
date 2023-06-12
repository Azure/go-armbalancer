package armbalancer

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const rateLimitHeaderPrefix = "X-Ms-Ratelimit-Remaining-"

type Options struct {
	Transport *http.Transport

	// Host is the only host that can be reached through the round tripper.
	// Default: management.azure.com
	Host string

	// PoolSize is the max number of connections that will be created by the connection pool.
	// Default: 8
	PoolSize int

	// RecycleThreshold is the lowest value of any X-Ms-Ratelimit-Remaining-* header that
	// can be seen before the associated connection will be re-established.
	// Default: 100
	RecycleThreshold int64

	// MinReqsBeforeRecycle is a safeguard to prevent frequent connection churn in the unlikely event
	// that a connections lands on an ARM instance that already has a depleted rate limiting quota.
	// Default: 10
	MinReqsBeforeRecycle int64

	// TransportFactory is a function that creates a new transport for a given connection.
	TransportFactory func(id int, parent *http.Transport, host string, port string, recycleThreshold, minReqsBeforeRecycle int64) http.RoundTripper
}

// New wraps a transport to provide smart connection pooling and client-side load balancing.
func New(opts Options) http.RoundTripper {
	if opts.Transport == nil {
		opts.Transport = http.DefaultTransport.(*http.Transport)
	}
	if opts.Host == "" {
		opts.Host = "management.azure.com"
	}
	if i := strings.Index(opts.Host, string(':')); i < 0 {
		opts.Host += ":443"
	}

	host, port, err := net.SplitHostPort(opts.Host)
	if err != nil {
		panic(fmt.Sprintf("invalid host %q: %s", host, err))
	}
	if host == "" {
		host = "management.azure.com"
	}
	if port == "" {
		port = "443"
	}
	if opts.PoolSize == 0 {
		opts.PoolSize = 8
	}
	if opts.RecycleThreshold == 0 {
		opts.RecycleThreshold = 100
	}
	if opts.MinReqsBeforeRecycle == 0 {
		opts.MinReqsBeforeRecycle = 10
	}

	if opts.TransportFactory == nil {
		opts.TransportFactory = newRecyclableTransport
	}

	t := &transportPool{pool: make([]http.RoundTripper, opts.PoolSize)}
	for i := range t.pool {
		t.pool[i] = newRecyclableTransport(i, opts.Transport, host, port, opts.RecycleThreshold, opts.MinReqsBeforeRecycle)
	}
	return t
}

type transportPool struct {
	pool   []http.RoundTripper
	cursor int64
}

func (t *transportPool) RoundTrip(req *http.Request) (*http.Response, error) {
	i := int(atomic.AddInt64(&t.cursor, 1)) % len(t.pool)
	return t.pool[i].RoundTrip(req)
}

type recyclableTransport struct {
	lock        sync.Mutex // only hold while copying pointer - not calling RoundTrip
	host        string
	port        string
	current     *http.Transport
	counter     int64 // atomic
	activeCount *sync.WaitGroup
	state       *connState
	signal      chan struct{}
}

func newRecyclableTransport(id int, parent *http.Transport, host string, port string, recycleThreshold, minReqsBeforeRecycle int64) http.RoundTripper {
	tx := parent.Clone()
	tx.MaxConnsPerHost = 1

	r := &recyclableTransport{
		host:        host,
		port:        port,
		current:     tx.Clone(),
		activeCount: &sync.WaitGroup{},
		state:       newConnState(),
		signal:      make(chan struct{}, 1),
	}
	go func() {
		for range r.signal {
			if r.state.Min() > recycleThreshold || atomic.LoadInt64(&r.counter) < minReqsBeforeRecycle {
				continue
			}

			// Swap a new transport in place while holding a pointer to the previous
			r.lock.Lock()
			previous := r.current
			previousActiveCount := r.activeCount
			r.current = tx.Clone()
			atomic.StoreInt64(&r.counter, 0)
			r.activeCount = &sync.WaitGroup{}
			r.lock.Unlock()

			// Wait for all active requests against the previous transport to complete before closing its idle connections
			previousActiveCount.Wait()
			previous.CloseIdleConnections()
		}
	}()
	return r
}

// return retrue if transport host matched with request host
func (t *recyclableTransport) compareHost(request *url.URL) bool {
	parsedHostName := request.Hostname()
	if t.host != parsedHostName {
		return false
	}
	if len(request.Host) == len(parsedHostName) {
		return true
	}
	return t.port == request.Port()
}

func (t *recyclableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	matched := t.compareHost(req.URL)
	if !matched {
		return nil, fmt.Errorf("host %q is not supported by the configured ARM balancer, supported host name is %q", req.URL.Host, t.host)
	}

	t.lock.Lock()
	tx := t.current
	wg := t.activeCount
	wg.Add(1)
	t.lock.Unlock()

	defer func() {
		t.lock.Lock()
		wg.Add(-1)
		t.lock.Unlock()
	}()

	resp, err := tx.RoundTrip(req)
	atomic.AddInt64(&t.counter, 1)

	if resp != nil {
		t.state.ApplyHeader(resp.Header)
	}

	select {
	case t.signal <- struct{}{}:
	default:
	}
	return resp, err
}

type connState struct {
	lock  sync.Mutex
	types map[string]int64
}

func newConnState() *connState {
	return &connState{types: make(map[string]int64)}
}

func (c *connState) ApplyHeader(h http.Header) {
	c.lock.Lock()
	for key, vals := range h {
		if !strings.HasPrefix(key, "X-Ms-Ratelimit-Remaining-") {
			continue
		}
		n, err := strconv.ParseInt(vals[0], 10, 0)
		if err != nil {
			continue
		}
		c.types[key[len(rateLimitHeaderPrefix):]] = n
	}
	c.lock.Unlock()
}

func (c *connState) Min() int64 {
	c.lock.Lock()
	var min int64 = math.MaxInt64
	for _, val := range c.types {
		if val < min {
			min = val
		}
	}
	c.lock.Unlock()
	return min
}
