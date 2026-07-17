package credentials

import (
	"context"
	"net"
	"time"
)

// LookupHostResilient attempts to resolve a hostname first using the default system DNS,
// and falls back to Cloudflare (1.1.1.1) and Google (8.8.8.8) public resolvers if it fails.
func LookupHostResilient(ctx context.Context, host string) ([]net.IP, error) {
	// 1. Try default system resolver first
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err == nil && len(ips) > 0 {
		return ips, nil
	}

	// 2. Fall back to Cloudflare (1.1.1.1)
	cfResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, "1.1.1.1:53")
		},
	}
	ips, err = cfResolver.LookupIP(ctx, "ip", host)
	if err == nil && len(ips) > 0 {
		return ips, nil
	}

	// 3. Fall back to Google (8.8.8.8)
	googleResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, "8.8.8.8:53")
		},
	}
	return googleResolver.LookupIP(ctx, "ip", host)
}

// DialContextResilient wraps net.Dialer.DialContext with resilient DNS fallback lookup.
func DialContextResilient(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return dialer.DialContext(ctx, network, addr)
	}

	if net.ParseIP(host) != nil {
		return dialer.DialContext(ctx, network, addr)
	}

	ips, err := LookupHostResilient(ctx, host)
	if err == nil && len(ips) > 0 {
		var lastErr error
		for _, ip := range ips {
			target := net.JoinHostPort(ip.String(), port)
			conn, err := dialer.DialContext(ctx, network, target)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
	}

	return dialer.DialContext(ctx, network, addr)
}
