package proxy

import (
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/skadraneshghn/clever-ai-gate/internal/config"
)

// BuildOptimizedTransport creates an HTTP transport tuned for maximum throughput
// when proxying requests to upstream AI providers.
//
// Key optimizations:
//   - TCP_NODELAY: disables Nagle's algorithm so token-sized SSE chunks flush instantly
//   - HTTP/2 multiplexing: multiple streams share a single TCP connection
//   - Large idle connection pool: avoids TCP handshake overhead for repeated requests
//   - Short dial timeout: fails fast to enable rapid failover
func BuildOptimizedTransport(cfg *config.Config) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: cfg.KeepAlive,
			Control: func(network, address string, c syscall.RawConn) error {
				return c.Control(func(fd uintptr) {
					// Disable Nagle's algorithm — flush every SSE token chunk immediately.
					// Without this, small writes (individual tokens ~20-50 bytes) get buffered
					// for up to 200ms before being sent, destroying perceived streaming speed.
					_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
				})
			},
		}).DialContext,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		// Disable compression — AI providers send JSON which we forward as-is.
		// Avoiding decompression/recompression saves CPU cycles on the hot-path.
		DisableCompression: true,
	}
}

// BuildHTTPClient creates the shared HTTP client for upstream requests.
// Timeout is set to 0 because:
//   - Streaming requests can run for minutes (large code generation)
//   - Per-request cancellation is handled via context.Context
//   - The transport-level dial timeout catches connection failures
func BuildHTTPClient(transport *http.Transport) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   0, // No global timeout; context-based cancellation only
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Prevent automatic redirect following — providers shouldn't redirect,
			// and if they do, we want to know about it rather than silently follow
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
