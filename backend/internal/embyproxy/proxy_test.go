package embyproxy

import (
	"strings"
	"testing"

	"curio/internal/models"
)

func TestTokenFromPlayURLUsesQueryToken(t *testing.T) {
	got := tokenFromPlayURL("/play/115/%E6%95%A6%E5%88%BB%E5%B0%94%E5%85%8B.iso?token=abc.def")
	if got != "abc.def" {
		t.Fatalf("expected query token, got %q", got)
	}
}

func TestTokenFromPlayURLKeepsLegacyPathToken(t *testing.T) {
	got := tokenFromPlayURL("/play/115/legacy.token")
	if got != "legacy.token" {
		t.Fatalf("expected legacy token, got %q", got)
	}
}

func TestTokenFromPlayURLUsesAbsoluteURLQueryToken(t *testing.T) {
	got := tokenFromPlayURL("http://localhost:8097/play/115/movie.iso?token=abc.def")
	if got != "abc.def" {
		t.Fatalf("expected query token, got %q", got)
	}
}

func TestTokenFromPlayURLKeepsNestedReadableRoute(t *testing.T) {
	got := tokenFromPlayURL("/play/115/collections/movies/Dunkirk%20(2017)/Dunkirk.iso")
	if got == "" || got == "collections" || !strings.Contains(got, "/") {
		t.Fatalf("expected nested route, got %q", got)
	}
}

func TestPlayRouteNameUsesRelativePath(t *testing.T) {
	got := playRouteName(models.STRMLink{
		RelativePath: "collections/movies/Dunkirk (2017)/Dunkirk.iso",
		STRMPath:     "/data/Curio/strm/collections/movies/Dunkirk (2017)/Dunkirk.strm",
	})
	if got != "collections/movies/Dunkirk (2017)/Dunkirk.iso" {
		t.Fatalf("unexpected route %q", got)
	}
}
