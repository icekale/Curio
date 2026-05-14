package worker

import (
	"strings"
	"testing"

	"curio/internal/scanner"
)

func TestSubtitleTargetUsesChineseLanguageSuffix(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Movie (2020).mkv", scanner.Sidecar{Name: "Movie (2020).zh-SG.srt", Extension: "srt"}, counts)
	if !strings.HasSuffix(target, ".chs.srt") {
		t.Fatalf("expected simplified suffix, got %s", target)
	}

	target = subtitleTarget("/library/Movie (2020).mkv", scanner.Sidecar{Name: "Movie (2020).zh-Hant.ass", Extension: "ass"}, counts)
	if !strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected traditional suffix, got %s", target)
	}
}

func TestSubtitleTargetKeepsDuplicateSubtitles(t *testing.T) {
	counts := map[string]int{}
	first := subtitleTarget("/library/Movie.mkv", scanner.Sidecar{Name: "Movie.zh-CN.srt", Extension: "srt"}, counts)
	second := subtitleTarget("/library/Movie.mkv", scanner.Sidecar{Name: "Movie.SC.srt", Extension: "srt"}, counts)
	if first == second || !strings.HasSuffix(second, ".chs.2.srt") {
		t.Fatalf("expected duplicate suffix, got first=%s second=%s", first, second)
	}
}

func TestTemplateWithImplicitCategoryKeepsMediaTopLevelFirst(t *testing.T) {
	values := map[string]string{"category": "欧美电影"}
	got := templateWithImplicitCategory("movies/{title}/{title}.{extension}", values)
	want := "movies/{category}/{title}/{title}.{extension}"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTemplateWithImplicitCategoryPrefixesCustomTemplate(t *testing.T) {
	values := map[string]string{"category": "欧美电影"}
	got := templateWithImplicitCategory("{title}/{title}.{extension}", values)
	want := "{category}/{title}/{title}.{extension}"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
