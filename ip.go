package websocket

import (
	"net"
	"sync"
)

var (
	// _ip stores the local IP address once it has been determined
	_ip net.IP

	// _ipOnce ensures the IP address is only determined once
	_ipOnce sync.Once
)

// IP returns the local IP address of the machine.
// It looks for the first non-loopback IPv4 address available on the system.
// The result is cached, so subsequent calls return the same value.
// @Description: Get local IP address
// @return net.IP The local IP address, or nil if none could be determined
func IP() net.IP {
	_ipOnce.Do(func() {
		as, _ := net.InterfaceAddrs()
		for _, a := range as {
			inet, ok := a.(*net.IPNet)
			if !ok || inet.IP.IsLoopback() {
				continue
			}
			ip := inet.IP.To4()
			if ip == nil {
				continue
			}
			_ip = ip
			return
		}
	})
	return _ip
}
