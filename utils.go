package armbalancer

import (
	"strings"
)

func getTransportHostToCompare(reqHost, transportHost string) string {
	idx := strings.Index(reqHost, ":")
	idx1 := strings.Index(transportHost, ":")

	// both host have ":" or not, no need to change transportHost
	if idx == idx1 {
		return transportHost
	}

	// reqHost has ":", but transportHost doesn't
	if idx != -1 {
		return transportHost + reqHost[idx:]
	}

	// reqHost doesn't have ":", but transportHost does
	return transportHost[:idx1]
}
