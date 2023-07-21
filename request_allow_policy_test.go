package armbalancer

import (
	"net/http"
	"net/url"
	"testing"
)

func Test_AcceptedRequestTargetAtHost(t *testing.T) {
	type fields struct {
		Transport *http.Transport
		host      string
		port      string
	}
	type args struct {
		request *url.URL
	}
	tests := []struct {
		name      string
		reqHost   string
		transHost string
		transPort string
		expected  bool
	}{
		{
			name:      "matched since all without port number",
			reqHost:   "host.com",
			transHost: "host.com",
			transPort: "443",
			expected:  true,
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
			name:      "matched with removing port name",
			reqHost:   "host.com",
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://"+tt.reqHost, nil)
			if err != nil {
				t.Errorf("AcceptedRequestTargetAtHost.Accepted() = %v, want %v", err, nil)
			}
			if got := AcceptedRequestTargetAtHost(tt.transHost, tt.transPort)(req); got != tt.expected {
				t.Errorf("case %s: AcceptedRequestTargetAtHost(%s,%s)(%s) = %v, want %v", tt.name, tt.transHost, tt.transPort, tt.reqHost, got, tt.expected)
			}
		})
	}
}
