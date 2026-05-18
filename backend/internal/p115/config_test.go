package p115

import "testing"

func TestParseLibraryCID(t *testing.T) {
	lib, err := ParseLibraryCID("3428557242282467406")
	if err != nil {
		t.Fatal(err)
	}
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

func TestParseLibraryCIDRejectsStructuredConfig(t *testing.T) {
	if _, err := ParseLibraryCID("cid: 3428557242282467406"); err == nil {
		t.Fatal("expected structured config to be rejected")
	}
	if _, err := ParseLibraryCID("3428557242282467406\n123"); err == nil {
		t.Fatal("expected multi-line config to be rejected")
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
