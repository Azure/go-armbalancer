package armbalancer

import "net/http"

// return retrue if transport host matched with request host
func AcceptedRequestTargetAtHost(host, port string) RequestAcceptPolicy {
	return func(request *http.Request) bool {
		return request.URL.Hostname() == host && (request.URL.Port() == "" || port == request.URL.Port())
	}
}
