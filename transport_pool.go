package armbalancer

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/sync/errgroup"
)

type transportChannPool struct {
	sync.WaitGroup
	capacity            chan struct{}
	pool                chan *http.Transport
	transportFactory    func() *http.Transport
	transportDropPolicy []TransportDropPolicy
	requestAcceptPolicy RequestAcceptPolicy
}

type RequestAcceptPolicy func(*http.Request) bool

type TransportDropPolicy interface {
	ShouldDropTransport(header http.Header) bool
}

type TransportDropPolicyFunc func(header http.Header) bool

func (function TransportDropPolicyFunc) ShouldDropTransport(header http.Header) bool {
	if function == nil {
		return false
	}
	return function(header)
}

func newtransportChannPool(size int, transportFactory func() *http.Transport, acceptPolicy RequestAcceptPolicy, dropPolicy ...TransportDropPolicy) *transportChannPool {
	if size <= 0 {
		return nil
	}
	pool := &transportChannPool{
		capacity:            make(chan struct{}, size),
		pool:                make(chan *http.Transport, size),
		transportFactory:    transportFactory,
		transportDropPolicy: dropPolicy,
		requestAcceptPolicy: acceptPolicy,
	}
	return pool
}

func (pool *transportChannPool) Run(ctx context.Context) error {
CLEANUP:
	for {
		select {
		case <-ctx.Done():
			break CLEANUP
		case pool.capacity <- struct{}{}:
			pool.pool <- pool.transportFactory()
		}
	}

	//cleanup
	close(pool.capacity) // no more transport is added. consumers will be released if channel is closed.
	errGroup := new(errgroup.Group)
	errGroup.Go(func() error {
		pool.Wait()      // wait for transport recycle loop
		close(pool.pool) // no more transport is added consumers will released if channel is closed.
		return nil
	})
	for transport := range pool.pool {
		transport := transport
		errGroup.Go(func() error {
			transport.CloseIdleConnections()
			return nil
		})
	}
	return errGroup.Wait() // close all of transports in pool
}

func (pool *transportChannPool) RoundTrip(req *http.Request) (*http.Response, error) {
	if pool.requestAcceptPolicy != nil && !pool.requestAcceptPolicy(req) {
		return nil, fmt.Errorf("the request is not supported by the configured ARM balancer")
	}

	transport, err := pool.selectTransport(req)
	if err != nil {
		return nil, err
	}
	resp, err := transport.RoundTrip(req)
	var header http.Header
	if resp != nil {
		header = resp.Header.Clone()
	}
	pool.Add(1)
	go pool.recycleTransport(transport, header)
	return resp, err
}

func (pool *transportChannPool) selectTransport(req *http.Request) (*http.Transport, error) {
	for {
		var t *http.Transport
		var ok bool
		select {
		case t, ok = <-pool.pool:
			if !ok {
				return nil, http.ErrServerClosed
			}
			return t, nil
		case <-req.Context().Done():
			return nil, http.ErrServerClosed
		}
	}
}

func (pool *transportChannPool) recycleTransport(t *http.Transport, header http.Header) {
	defer pool.Done()
	for _, policy := range pool.transportDropPolicy {
		if policy.ShouldDropTransport(header) {
			t.Clone().CloseIdleConnections() // drop the transport
			<-pool.capacity                  // notify pool to create new transport
			return
		}
	}
	pool.pool <- t
}
