package p115

import (
	"strings"
	"testing"

	"curio/internal/models"
)

func TestPlayURLForLinkNameUsesReadableDisplayPathWithoutToken(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-1", "http://localhost:8080", "电影/敦刻尔克 (2017) - 2160p UHD HEVC DTS-HD MA.iso")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(playURL, "token=") {
		t.Fatalf("expected token-free play url, got %q", playURL)
	}
	if strings.Contains(playURL, "%E6%") {
		t.Fatalf("expected readable utf-8 play url, got %q", playURL)
	}
	if strings.Contains(playURL, " ") {
		t.Fatalf("expected spaces to be escaped for player compatibility, got %q", playURL)
	}
	if !strings.Contains(playURL, "/play/115/电影/敦刻尔克%20(2017)%20-%202160p%20UHD%20HEVC%20DTS-HD%20MA.iso") {
		t.Fatalf("expected chinese display path, got %q", playURL)
	}
}

func TestPlayURLForLinkNameFallsBackToIDRoute(t *testing.T) {
	service := NewService(nil)
	playURL, err := service.PlayURLForLinkName("link-2", "http://localhost:8080", "")
	if err != nil {
		t.Fatal(err)
	}
	if playURL != "http://localhost:8080/play/115/id/link-2" {
		t.Fatalf("unexpected fallback play url %q", playURL)
	}
}

func TestPlayBaseURLFallsBackToEmbyPublicURL(t *testing.T) {
	got := playBaseURL(models.P115Settings{EmbyPublicURL: "http://192.168.10.83:8097"}, "http://localhost:8080")
	if got != "http://192.168.10.83:8097" {
		t.Fatalf("unexpected play base url %q", got)
	}
}
