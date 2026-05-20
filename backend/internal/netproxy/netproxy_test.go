package netproxy

import "testing"

func TestParseAcceptsHTTPAndSOCKSWithAuth(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:7890",
		"https://user:pass@proxy.example:8443",
		"socks5://user:pass@127.0.0.1:1080",
		"socks5h://127.0.0.1:1080",
		"sock5://127.0.0.1:1080",
	} {
		if _, err := Parse(raw); err != nil {
			t.Fatalf("expected proxy %q to be accepted: %v", raw, err)
		}
	}
}

func TestParseRejectsUnsupportedProxyScheme(t *testing.T) {
	if _, err := Parse("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected unsupported proxy scheme to be rejected")
	}
}
