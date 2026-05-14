package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAttachSidecarsFromSubtitleDirectoryForSingleMovie(t *testing.T) {
	files := []File{{
		Path: "/media/Movie (2020)/Movie (2020).mkv",
		Name: "Movie (2020).mkv",
	}}
	sidecars := []Sidecar{
		{Path: "/media/Movie (2020)/Subs/zh-cn.srt", Name: "zh-cn.srt", Extension: "srt"},
		{Path: "/media/Movie (2020)/Movie (2020).chs.ass", Name: "Movie (2020).chs.ass", Extension: "ass"},
	}

	got := AttachSidecars(files, sidecars)
	if len(got[0].Sidecars) != 2 {
		t.Fatalf("expected 2 sidecars, got %d", len(got[0].Sidecars))
	}
}

func TestAttachSidecarsFromSubtitleDirectoryByEpisodeToken(t *testing.T) {
	files := []File{
		{Path: "/show/Show.S01E01.mkv", Name: "Show.S01E01.mkv"},
		{Path: "/show/Show.S01E02.mkv", Name: "Show.S01E02.mkv"},
	}
	sidecars := []Sidecar{
		{Path: "/show/Subs/Show.S01E02.zh-cn.srt", Name: "Show.S01E02.zh-cn.srt", Extension: "srt"},
	}

	got := AttachSidecars(files, sidecars)
	if len(got[0].Sidecars) != 0 {
		t.Fatalf("expected episode 1 to have no sidecars, got %d", len(got[0].Sidecars))
	}
	if len(got[1].Sidecars) != 1 {
		t.Fatalf("expected episode 2 sidecar, got %d", len(got[1].Sidecars))
	}
}

func TestScanSkipsNonMediaResources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.nfo"), []byte("metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Scan(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no state-machine files for non-media resources")
	}
}
