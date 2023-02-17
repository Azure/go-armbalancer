package armbalancer

import (
	"testing"
)

type testCase struct {
	name      string
	reqHost   string
	transHost string
	expected  string
}

func TestGetTransportHostToCompare(t *testing.T) {
	cases := []testCase{
		{
			name:      "no modify since all without port number",
			reqHost:   "host.com",
			transHost: "host.com",
			expected:  "host.com",
		},
		{
			name:      "no modify since all with port number",
			reqHost:   "host.com:443",
			transHost: "host.com:11254",
			expected:  "host.com:11254",
		},
		{
			name:      "append port name",
			reqHost:   "host.com:443",
			transHost: "host.com",
			expected:  "host.com:443",
		},
		{
			name:      "remove port name",
			reqHost:   "host.com",
			transHost: "host.com:443",
			expected:  "host.com",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := getTransportHostToCompare(c.reqHost, c.transHost)
			if v != c.expected {
				t.Errorf("expected host %q is not same as we get %q", c.expected, v)
			}
		})
	}
}
