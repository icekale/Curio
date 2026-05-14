package p115

import "testing"

func TestParseLibrariesPlainCID(t *testing.T) {
	cfg, err := ParseLibraries("3428557242282467406")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Libraries) != 1 {
		t.Fatalf("expected 1 library, got %d", len(cfg.Libraries))
	}
	lib := cfg.Libraries[0]
	if lib.CID != "3428557242282467406" {
		t.Fatalf("unexpected cid %q", lib.CID)
	}
	if lib.OutputPrefix != "" {
		t.Fatalf("plain cid should not add output prefix, got %q", lib.OutputPrefix)
	}
	if lib.LayerLimit != 25 {
		t.Fatalf("unexpected layer limit %d", lib.LayerLimit)
	}
}

func TestNormalizeCookieLoginApp(t *testing.T) {
	if got := NormalizeCookieLoginApp("qandriod"); got != "qandroid" {
		t.Fatalf("unexpected normalized app %q", got)
	}
	if got := NormalizeCookieLoginApp(""); got != "wechatmini" {
		t.Fatalf("unexpected default app %q", got)
	}
}
