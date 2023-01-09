# ARM Balancer

A client-side connection manager for Azure Resource Manager.


## Why?

ARM request throttling is scoped to the specific instance of ARM that a connection lands on.
This serves to reduce the risk of a noisy client impacting the performance of other requests handled concurrently by that particular instance without requiring coordination between instances.

HTTP1.1 clients commonly use pooled TCP connections to provide concurrency.
But HTTP2 allows a single connection to handle many concurrent requests.
Conforming client implementations will only open a second connection when the concurrency limit advertised by the server would be exceeded.

This poses a problem for ARM consumers using HTTP2: requests that were previously distributed across several ARM instances will now be sent to only one.


## Design

- Multiple connections are established with ARM, forming a simple client-side load balancer
- Connections are re-established when they receive a "ratelimit-remaining" header below a certain threshold

This scheme avoids throttling by proactively redistributing load across ARM instances.
Performance under high concurrency may also improve relative to HTTP1.1 since the pool of connections can easily be made larger than common HTTP client defaults.


## Usage

```go
armresources.NewClient("{{subscriptionID}}", cred, &arm.ClientOptions{
	ClientOptions: policy.ClientOptions{
		Transport: &http.Client{
			Transport: armbalancer.New(armbalancer.Options{}),
		},
	},
})
```
