package shortcut

import "testing"

// TestCheckURLHostUserinfoBypass locks the allowlist host check against the
// parser-differential bypass: a hand-rolled splitter read good.com:80@evil.com as
// good.com (allowlisted) while Go actually dials evil.com. The check must use the
// SAME host net/url (and thus the dialer + redirect handling) uses.
func TestCheckURLHostUserinfoBypass(t *testing.T) {
	allow := []string{"good.com", "api.example.com"}
	reject := []string{
		"http://good.com:80@evil.com/steal", // userinfo bypass — real host is evil.com
		"http://good.com@evil.com/",         // userinfo bypass
		"https://evil.com/",                 // simply not allowlisted
		"https://good.com.evil.com/",        // suffix trick
		"https://notgood.com/",              // substring trick
		"https://{region}.api.com/",         // unresolved host placeholder → no host
	}
	for _, u := range reject {
		if err := checkURLHost(u, allow); err == nil {
			t.Errorf("checkURLHost(%q) = nil, want rejection", u)
		}
	}

	accept := []string{
		"https://good.com/path",                 // exact
		"https://api.example.com/q={city}",      // placeholder in query is fine
		"https://api.example.com:8443/v1?a={x}", // explicit port is stripped
		"https://sub.good.com/",                 // subdomain (suffix match)
	}
	for _, u := range accept {
		if err := checkURLHost(u, allow); err != nil {
			t.Errorf("checkURLHost(%q) = %v, want allow", u, err)
		}
	}
}
