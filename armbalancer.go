package armbalancer

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const rateLimitHeaderPrefix = "X-Ms-Ratelimit-Remaining-"

// New wraps a transport to provide smart connection pooling and client-side load balancing.
// Only valid for requests to the given host.
//
// Connections are recycled when recycleThreshold is <= the lowest value of any X-Ms-Ratelimit-Remaining-* header.
//
// minReqsBeforeRecycle is a safeguard to prevent frequent connection churn in the unlikely event
// that a connections lands on an ARM instance that already has a depleted rate limiting quota.
func New(transport *http.Transport, host string, poolSize, recycleThreshold, minReqsBeforeRecycle int64) http.RoundTripper {
	t := &transportPool{pool: make([]http.RoundTripper, poolSize)}
	for i := range t.pool {
		t.pool[i] = newRecyclableTransport(i, transport, host, recycleThreshold, minReqsBeforeRecycle)
	}
	return t
}

// NewWithDefaults calls New with sane default values for most arguments.
func NewWithDefaults(transport *http.Transport) http.RoundTripper {
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	return New(transport, "management.azure.com", 8, 100, 10)
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
	current     *http.Transport
	counter     int64 // atomic
	activeCount *sync.WaitGroup
	state       *connState
	signal      chan struct{}
}

func newRecyclableTransport(id int, parent *http.Transport, host string, recycleThreshold, minReqsBeforeRecycle int64) *recyclableTransport {
	tx := parent.Clone()
	tx.MaxConnsPerHost = 1
	r := &recyclableTransport{
		host:        host,
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

func (t *recyclableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != t.host {
		return nil, fmt.Errorf("host %q is not supported by the configured ARM balancer", req.URL.Host)
	}

	t.lock.Lock()
	tx := t.current
	wg := t.activeCount
	t.lock.Unlock()

	wg.Add(1)
	defer wg.Add(-1)
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
	defer c.lock.Unlock()

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
}

func (c *connState) Min() int64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	var min int64 = math.MaxInt64
	for _, val := range c.types {
		if val < min {
			min = val
		}
	}
	return min
}
