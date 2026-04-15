package trustedproxy

import (
	"net"
	"net/http"
	"os"
	"strings"
)

func ClientIP(r *http.Request, trustForwarded bool, envVar string) string {
	if r == nil {
		return ""
	}

	if canTrustForwardedHeaders(r, trustForwarded, envVar) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			for _, ip := range strings.Split(xff, ",") {
				if candidate := strings.TrimSpace(ip); candidate != "" {
					return candidate
				}
			}
		}

		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			return xri
		}

		if cfip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfip != "" {
			return cfip
		}
	}

	return RemoteIP(r.RemoteAddr)
}

func PriorForwardedFor(r *http.Request, trustForwarded bool, envVar string) string {
	if r == nil || !canTrustForwardedHeaders(r, trustForwarded, envVar) {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
}

func ForwardedProto(r *http.Request, trustForwarded bool, envVar string) string {
	if r != nil && canTrustForwardedHeaders(r, trustForwarded, envVar) {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			return proto
		}
	}
	if r != nil && r.TLS != nil {
		return "https"
	}
	return "http"
}

func ForwardedHost(r *http.Request, trustForwarded bool, envVar string) string {
	if r != nil && canTrustForwardedHeaders(r, trustForwarded, envVar) {
		if host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); host != "" {
			return host
		}
	}
	if r == nil {
		return ""
	}
	return r.Host
}

func RemoteIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.Trim(host, "[]")
	}

	return strings.Trim(remoteAddr, "[]")
}

func canTrustForwardedHeaders(r *http.Request, trustForwarded bool, envVar string) bool {
	if r == nil || !trustForwarded {
		return false
	}

	peer := RemoteIP(r.RemoteAddr)
	if peer == "" {
		return false
	}

	ip := net.ParseIP(peer)
	if ip == nil {
		return false
	}

	trustedCIDRs := parseCIDRs(os.Getenv(envVar))
	if len(trustedCIDRs) == 0 {
		return false
	}

	for _, cidr := range trustedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

func parseCIDRs(raw string) []*net.IPNet {
	var cidrs []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, network, err := net.ParseCIDR(part)
		if err == nil && network != nil {
			cidrs = append(cidrs, network)
		}
	}
	return cidrs
}
