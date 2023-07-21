package armbalancer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/sync/errgroup"
)

func Test_transportChannPool_Run(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	}))
	defer ts.Close()

	pool := newtransportChannPool(2, func() *http.Transport {
		return http.DefaultTransport.(*http.Transport).Clone()
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var serverGrp errgroup.Group
	serverGrp.SetLimit(-1)
	serverGrp.Go(func() error {
		return pool.Run(ctx)
	})

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal("http.NewRequest should not return error")
	}
	resp, err := pool.RoundTrip(req)
	if err != nil {
		t.Fatalf("http.RoundTrip should not return error, got: %+v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatal("http.RoundTrip should return ok")
	}
	resp.Body.Close()
	cancel()
	if err := serverGrp.Wait(); err != nil {
		t.Fatal(err)
	}
}

func Benchmark_testRoundtripperPool(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	}))
	defer ts.Close()

	pool := newtransportChannPool(2, func() *http.Transport {
		return http.DefaultTransport.(*http.Transport).Clone()
	}, nil)
	ctx, cancelFn := context.WithCancel(context.Background())
	var serverGrp errgroup.Group
	serverGrp.Go(func() error {
		return pool.Run(ctx)
	})
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", ts.URL, nil)
			resp, err := pool.RoundTrip(req)
			if err != nil {
				b.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				b.Fatal("http.RoundTrip should return ok")
			}
			resp.Body.Close()
		}
	})

	b.StopTimer()
	cancelFn()

	if err := serverGrp.Wait(); err != nil {
		b.Fatal(err)
	}
}
