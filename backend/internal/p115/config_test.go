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
		t.Fatalf("plain cid should not add synthetic output prefix, got %q", lib.OutputPrefix)
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

func TestParseLibraryCIDs(t *testing.T) {
	libs, err := ParseLibraryCIDs("3428557242282467406\n1234567890, 9876543210")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 3 {
		t.Fatalf("unexpected library count %d", len(libs))
	}
	if libs[0].CID != "3428557242282467406" || libs[1].CID != "1234567890" || libs[2].CID != "9876543210" {
		t.Fatalf("unexpected libraries %#v", libs)
	}
	if libs[0].Name != "媒体库 1" || libs[2].Name != "媒体库 3" {
		t.Fatalf("unexpected library names %#v", libs)
	}
	if libs[0].OutputPrefix != "" || libs[1].OutputPrefix != "" || libs[2].OutputPrefix != "" {
		t.Fatalf("unexpected library output prefixes %#v", libs)
	}
}

func TestParseLibraryCIDsDeduplicates(t *testing.T) {
	libs, err := ParseLibraryCIDs("3428557242282467406\n3428557242282467406")
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].Name != "媒体库" {
		t.Fatalf("unexpected libraries %#v", libs)
	}
	if libs[0].OutputPrefix != "" {
		t.Fatalf("single library should not add synthetic output prefix, got %q", libs[0].OutputPrefix)
	}
	if got := FormatLibraryCIDs(libs); got != "3428557242282467406" {
		t.Fatalf("unexpected formatted cids %q", got)
	}
}

func TestApplyLibraryOutputRootsPairsByLine(t *testing.T) {
	libs, err := ParseLibraryCIDs("3429318291990438503\n3429318291990438504")
	if err != nil {
		t.Fatal(err)
	}
	libs, err = ApplyLibraryOutputRoots(libs, "/data/Curio/strm\n/data/Curio/strm2")
	if err != nil {
		t.Fatal(err)
	}
	if libs[0].OutputRoot != "/data/Curio/strm" || libs[1].OutputRoot != "/data/Curio/strm2" {
		t.Fatalf("unexpected output roots %#v", libs)
	}
	if got := FormatLibraryOutputRoots(libs); got != "/data/Curio/strm\n/data/Curio/strm2" {
		t.Fatalf("unexpected formatted output roots %q", got)
	}
}

func TestApplyLibraryOutputRootsSharesSinglePath(t *testing.T) {
	libs, err := ParseLibraryCIDs("3429318291990438503\n3429318291990438504")
	if err != nil {
		t.Fatal(err)
	}
	libs, err = ApplyLibraryOutputRoots(libs, "/data/Curio/strm")
	if err != nil {
		t.Fatal(err)
	}
	if libs[0].OutputRoot != "/data/Curio/strm" || libs[1].OutputRoot != "/data/Curio/strm" {
		t.Fatalf("unexpected output roots %#v", libs)
	}
	if got := FormatLibraryOutputRoots(libs); got != "/data/Curio/strm" {
		t.Fatalf("unexpected formatted shared output root %q", got)
	}
}

func TestApplyLibraryOutputRootsRejectsMismatchedCount(t *testing.T) {
	libs, err := ParseLibraryCIDs("3429318291990438503\n3429318291990438504")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLibraryOutputRoots(libs, "/data/Curio/strm\n/data/Curio/strm2\n/data/Curio/strm3"); err == nil {
		t.Fatal("expected mismatched output root count to be rejected")
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
