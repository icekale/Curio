package matcher

import "testing"

func TestNormalizeTitleKeepsChinese(t *testing.T) {
	if got := NormalizeTitle("长安三万里.2023"); got != "长安三万里 2023" {
		t.Fatalf("unexpected normalized title: %q", got)
	}
}

func TestNormalizeTitleFoldsApostropheAndAmpersand(t *testing.T) {
	if got := NormalizeTitle("It's Magic & Charlie Brown"); got != "its magic and charlie brown" {
		t.Fatalf("unexpected normalized title: %q", got)
	}
}
