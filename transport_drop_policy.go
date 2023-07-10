package armbalancer

import (
	"net/http"
	"strconv"
	"strings"
)

type KillBeforeThrottledPolicy struct {
	RecycleThreshold int64
}

func (policy *KillBeforeThrottledPolicy) ShouldDropTransport(header http.Header) bool {
	for key, vals := range header {
		if !strings.HasPrefix(key, "X-Ms-Ratelimit-Remaining-") {
			continue
		}
		n, err := strconv.ParseInt(vals[0], 10, 0)
		if err != nil {
			continue
		}
		if n < policy.RecycleThreshold {
			return true
		}
	}
	return false
}
