package gateway

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// TrustedProxies определяет, каким сетевым узлам разрешено передавать client IP.
type TrustedProxies struct{ networks []*net.IPNet }

func NewTrustedProxies(cidrs []string) (*TrustedProxies, error) {
	result := &TrustedProxies{}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", cidr, err)
		}
		result.networks = append(result.networks, network)
	}
	return result, nil
}
func (t *TrustedProxies) Contains(ip net.IP) bool {
	if t == nil {
		return false
	}
	for _, network := range t.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP игнорирует forwarded headers от недоверенного непосредственного peer.
func ClientIP(r *http.Request, trusted *TrustedProxies) string {
	peer := remoteIP(r.RemoteAddr)
	if !trusted.Contains(net.ParseIP(peer)) {
		return peer
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(chain[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !trusted.Contains(ip) {
			return candidate
		}
	}
	return peer
}
func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}
