package execrt

import (
	"net"
	"testing"
)

// TestIsPrivateIP locks the SSRF IP guard against the ranges Go's net helpers do
// NOT classify as private — most importantly the CGNAT range that hosts Alibaba
// Cloud's metadata service (100.100.100.200). A regression here re-opens
// cloud-credential theft from a single malicious plugin.
func TestIsPrivateIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", // loopback
		"169.254.169.254",             // AWS/GCP/Azure metadata (link-local)
		"100.100.100.200",             // Alibaba Cloud metadata (CGNAT)
		"100.64.0.1", "100.127.255.1", // RFC 6598 CGNAT range
		"10.0.0.5", "192.168.1.1", "172.16.0.1", // RFC1918
		"0.0.0.0",                      // unspecified
		"198.18.0.1",                   // benchmarking
		"240.0.0.1", "255.255.255.255", // reserved / broadcast
		"::ffff:169.254.169.254", // IPv4-mapped metadata
		"::ffff:100.100.100.200", // IPv4-mapped Alibaba metadata
		"::ffff:10.0.0.1",        // IPv4-mapped RFC1918
		"fc00::1",                // IPv6 ULA (private)
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: cannot parse %q", s)
		}
		if !isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = false, want true (SSRF guard must block it)", s)
		}
	}

	allowed := []string{
		"8.8.8.8", "1.1.1.1", // public DNS
		"93.184.216.34",        // example.com
		"2606:4700:4700::1111", // public IPv6 (Cloudflare)
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("test bug: cannot parse %q", s)
		}
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = true, want false (must not block legitimate public hosts)", s)
		}
	}
}
