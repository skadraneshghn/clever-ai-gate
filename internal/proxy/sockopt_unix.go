//go:build !windows

package proxy

import "syscall"

// setTCPNoDelay disables Nagle's algorithm on the given socket file descriptor.
// On Unix systems, syscall.SetsockoptInt expects the fd as an int.
func setTCPNoDelay(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
}
