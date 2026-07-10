package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"go.uber.org/zap"
)

// BuildOptimizedTransport creates an HTTP transport tuned for maximum throughput
// when proxying requests to upstream AI providers.
//
// Key optimizations:
//   - TCP_NODELAY: disables Nagle's algorithm so token-sized SSE chunks flush instantly
//   - TCP_QUICKACK: prevents delayed-ACK stalling on streaming responses (Linux)
//   - TCP_FASTOPEN_CONNECT: saves 1 RTT on reconnection (Linux 4.11+)
//   - HTTP/2 multiplexing: multiple streams share a single TCP connection
//   - Large idle connection pool: avoids TCP handshake overhead for repeated requests
//   - Edge IP probing: routes to the lowest-latency Anycast CDN edge node
//   - Short dial timeout: fails fast to enable rapid failover
//
// Returns the transport and an EdgeProber that must be Start()'ed and Stop()'ed
// by the caller for background probing and pre-warming.
func BuildOptimizedTransport(cfg *config.Config, logger *zap.Logger) (*http.Transport, *EdgeProber) {
	prober := NewEdgeProber(cfg.EdgeProbeHosts, logger)

	// Shared dialer with kernel-level socket tuning applied via Control.
	// Used for both DialContext (plain TCP) and DialTLSContext (TCP + TLS).
	applySockopts := func(network, address string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			_ = setTCPNoDelay(fd)         // Flush SSE tokens instantly
			_ = setTCPQuickAck(fd)        // Prevent ACK stalling (Linux)
			_ = setTCPFastOpenConnect(fd) // 1-RTT reconnects (Linux 4.11+)
		})
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,

		// DialContext handles plain TCP (non-TLS) connections, e.g. local
		// Ollama instances. No IP interception — local hosts don't need it.
		DialContext: (&net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: cfg.KeepAlive,
			Control:   applySockopts,
		}).DialContext,

		// DialTLSContext handles all HTTPS connections. This is where we
		// intercept the address to route through the probed fastest edge IP
		// while preserving the original hostname for TLS SNI verification.
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Determine the dial target: fastest probed IP, or original address.
			target := addr
			if ip := prober.GetFastestIP(host); ip != "" {
				target = net.JoinHostPort(ip, port)
			}

			conn, err := dialTLS(ctx, network, target, host, cfg, applySockopts)
			if err != nil && target != addr {
				// Fastest IP failed — fall back to normal DNS resolution.
				logger.Debug("edge IP failed, falling back to DNS",
					zap.String("host", host),
					zap.String("failed_ip", target),
					zap.Error(err),
				)
				conn, err = dialTLS(ctx, network, addr, host, cfg, applySockopts)
			}
			return conn, err
		},

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

	return transport, prober
}

// dialTLS establishes a TCP connection (with socket tuning) and performs a TLS
// handshake using hostname as the SNI ServerName. This allows dialing a
// specific IP while still validating the certificate against the domain.
func dialTLS(
	ctx context.Context,
	network, addr, hostname string,
	cfg *config.Config,
	control func(network, address string, c syscall.RawConn) error,
) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   cfg.DialTimeout,
		KeepAlive: cfg.KeepAlive,
		Control:   control,
	}

	rawConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	tlsConn := tls.Client(rawConn, &tls.Config{
		ServerName:         hostname,
		NextProtos:         []string{"h2", "http/1.1"},
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	})

	// Apply a TLS handshake timeout since DialTLSContext bypasses
	// the transport's TLSHandshakeTimeout setting.
	handshakeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		rawConn.Close()
		return nil, err
	}

	return tlsConn, nil
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
