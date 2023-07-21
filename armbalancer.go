package armbalancer

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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

	return newtransportChannPool(opts.PoolSize, func() *http.Transport {
		return opts.Transport.Clone()
	}, AcceptedRequestTargetAtHost(host, port), &KillBeforeThrottledPolicy{opts.RecycleThreshold})
}
