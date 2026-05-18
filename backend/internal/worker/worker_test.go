package worker

import (
	"path/filepath"
	"strings"
	"testing"

	"curio/internal/aifilename"
	"curio/internal/models"
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

func TestParsedFromAIKeepsTwoInOneMovieAsMovie(t *testing.T) {
	parsed, err := parsedFromAI(scanner.File{
		Path:      "/media/Alien.3.1992.Theatrical.Version.2003.Special.Edition.2in1.1080p.BluRay.AVC.DTS-HD.MA5.1-NGB.iso",
		Name:      "Alien.3.1992.Theatrical.Version.2003.Special.Edition.2in1.1080p.BluRay.AVC.DTS-HD.MA5.1-NGB.iso",
		Extension: "iso",
	}, aifilename.Analysis{
		MediaType:     models.MediaMovie,
		Title:         "Alien 3",
		Year:          1992,
		Resolution:    "1080p",
		Source:        "BluRay",
		VideoCodec:    "AVC",
		AudioCodec:    "DTS-HD MA",
		AudioChannels: "5.1",
		Edition:       "Theatrical Version / Special Edition / 2in1",
		Confidence:    0.94,
	})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.IsTV || parsed.Title != "Alien 3" || parsed.Year != 1992 || parsed.Episode != 0 {
		t.Fatalf("unexpected AI movie parse: %+v", parsed)
	}
	if parsed.Parser != "curio-ai" || parsed.Confidence != 94 {
		t.Fatalf("unexpected parser metadata: %+v", parsed)
	}
}

func TestParsedFromAIIgnoresTechnicalFields(t *testing.T) {
	parsed, err := parsedFromAI(scanner.File{
		Path:      "/media/Example.Movie.2024.iso",
		Name:      "Example.Movie.2024.iso",
		Extension: "iso",
	}, aifilename.Analysis{
		MediaType:     models.MediaMovie,
		Title:         "Example Movie",
		Year:          2024,
		Resolution:    "4K",
		Source:        "USA UHD Blu-ray",
		VideoCodec:    "H.265",
		AudioCodec:    "TrueHD Atmos",
		AudioChannels: "7.1",
		HDRFormat:     "DV HDR",
		Confidence:    0.94,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !unknownValue(parsed.Resolution) || !unknownValue(parsed.VideoCodec) || !unknownValue(parsed.AudioCodec) || !unknownValue(parsed.AudioChannels) || !unknownValue(parsed.HDRFormat) {
		t.Fatalf("AI technical fields should not be used for media facts: %+v", parsed)
	}
}

func TestRearchiveSourcePathUsesActualCloudPathOverride(t *testing.T) {
	file := models.MediaFile{
		CurrentPath: "cd2:///115open/staging/movies/Movie (2024)/Movie (2024) - 2160p H.265.mkv",
		FinalPath:   "cd2:///115open/staging/movies/Movie (2024)/Movie (2024) - 2160p H.265.mkv",
	}
	got := rearchiveSourcePath(file, "/115open/media/movies/Movie (2024)/Movie (2024) - 2160p H.265.mkv")
	want := "cd2:///115open/media/movies/Movie (2024)/Movie (2024) - 2160p H.265.mkv"
	if got != want {
		t.Fatalf("source override = %q, want %q", got, want)
	}
}

func TestApplyRearchiveTargetRootOverridesCloudStagingRoot(t *testing.T) {
	dirs := models.DirectoryConfig{
		IncomingPath:              "/115open/incoming",
		StagingPath:               "/115open/staging",
		FailedPath:                "/115open/Curio/failed",
		IncompleteCollectionsPath: "/115open/Curio/incomplete_collections",
	}
	file := models.MediaFile{CurrentPath: "cd2:///115open/media/movies/Movie.mkv"}
	if err := applyRearchiveTargetRoot(&dirs, file, "/115open/media"); err != nil {
		t.Fatal(err)
	}
	if dirs.StagingPath != "/115open/media" {
		t.Fatalf("staging root = %q, want /115open/media", dirs.StagingPath)
	}
}

func TestSelectRearchiveSidecarsIncludesLooseSameDirSubtitleForSingleMedia(t *testing.T) {
	source := "/library/Movie (1984)/Movie (1984) - 720p AVC AAC.mkv"
	sidecar := "/library/Movie (1984)/Movie (1984) - 720P X264 AAC.chs.ass"
	got := selectRearchiveSidecars([]scanner.File{{
		Path:      source,
		Name:      "Movie (1984) - 720p AVC AAC.mkv",
		Extension: "mkv",
	}}, []scanner.Sidecar{{
		Path:      sidecar,
		Name:      "Movie (1984) - 720P X264 AAC.chs.ass",
		Extension: "ass",
	}}, source, false)
	if len(got) != 1 || got[0].Path != sidecar {
		t.Fatalf("expected single loose same-dir sidecar, got %#v", got)
	}
}

func TestSelectRearchiveSidecarsDoesNotStealLooseSubtitleWhenDirectoryHasMultipleMedia(t *testing.T) {
	source := "/library/Show/Show - S01E01.mkv"
	got := selectRearchiveSidecars([]scanner.File{
		{Path: source, Name: "Show - S01E01.mkv", Extension: "mkv"},
		{Path: "/library/Show/Show - S01E02.mkv", Name: "Show - S01E02.mkv", Extension: "mkv"},
	}, []scanner.Sidecar{{
		Path:      "/library/Show/Commentary.ass",
		Name:      "Commentary.ass",
		Extension: "ass",
	}}, source, false)
	if len(got) != 0 {
		t.Fatalf("expected no loose sidecar in multi-media directory, got %#v", got)
	}
}

func TestCleanupStopRootUsesNearestCloudRoot(t *testing.T) {
	dirs := models.DirectoryConfig{
		IncomingPath:              "/115open/incoming",
		StagingPath:               "/115open/media",
		FailedPath:                "/115open/Curio/failed",
		IncompleteCollectionsPath: "/115open/Curio/incomplete_collections",
	}
	got := cleanupStopRoot(dirs, "/115open/Curio/incomplete_collections/movies/Series/Movie", true)
	if got != "/115open/Curio/incomplete_collections" {
		t.Fatalf("cleanup root = %q", got)
	}
	got = cleanupStopRoot(dirs, "/115open/staging/movies/Action/Movie", true)
	if got != "/115open/staging" {
		t.Fatalf("fallback cleanup root = %q", got)
	}
}

func TestSubtitleTargetUsesCompactAnimeSubtitleTags(t *testing.T) {
	counts := map[string]int{}
	simplified := subtitleTarget("/library/Apocalypse Hotel - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/[Nekomoe kissaten] Apocalypse Hotel [01].JPSC.ass",
		Name:      "[Nekomoe kissaten] Apocalypse Hotel [01].JPSC.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(simplified, ".chs.ass") {
		t.Fatalf("expected JPSC to become simplified suffix, got %s", simplified)
	}

	traditional := subtitleTarget("/library/Apocalypse Hotel - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/[Nekomoe kissaten] Apocalypse Hotel [01].JPTC.ass",
		Name:      "[Nekomoe kissaten] Apocalypse Hotel [01].JPTC.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(traditional, ".cht.ass") {
		t.Fatalf("expected JPTC to become traditional suffix, got %s", traditional)
	}
}

func TestSubtitleTargetUsesLanguageDirectoryHint(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Show - S01E01.mkv", scanner.Sidecar{
		Path:      "/incoming/Show/Subs/繁體/Show - S01E01.ass",
		Name:      "Show - S01E01.ass",
		Extension: "ass",
	}, counts)
	if !strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected directory language hint, got %s", target)
	}
}

func TestSubtitleTargetIgnoresEmbeddedSCLetters(t *testing.T) {
	counts := map[string]int{}
	target := subtitleTarget("/library/Show - S01E01.mkv", scanner.Sidecar{Name: "discussion.ass", Extension: "ass"}, counts)
	if strings.HasSuffix(target, ".chs.ass") || strings.HasSuffix(target, ".cht.ass") {
		t.Fatalf("expected no language suffix from embedded letters, got %s", target)
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

func TestRepairCollectionSourceRootFromLocalPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "incomplete_collections")
	sourceRoot := filepath.Join(root, "欧美电影", "指环王（系列）")
	paths := []string{
		filepath.Join(sourceRoot, "指环王1", "movie1.iso"),
		filepath.Join(sourceRoot, "指环王2", "movie2.iso"),
		filepath.Join(sourceRoot, "指环王3", "movie3.iso"),
	}
	got := repairCollectionSourceRoot(paths, root, false)
	if got != sourceRoot {
		t.Fatalf("expected %q, got %q", sourceRoot, got)
	}
	values := repairCollectionValues(models.CollectionMetadata{TMDBID: 119, Name: "指环王（系列）"}, got, root, false)
	if values["category"] != "欧美电影" {
		t.Fatalf("expected category, got %#v", values)
	}
}

func TestRepairCollectionSourceRootFromCloudPaths(t *testing.T) {
	root := "/115open/Curio/incomplete_collections"
	sourceRoot := "/115open/Curio/incomplete_collections/欧美电影/指环王（系列）"
	paths := []string{
		"cd2://115open/Curio/incomplete_collections/欧美电影/指环王（系列）/指环王1/movie1.iso",
		"cd2://115open/Curio/incomplete_collections/欧美电影/指环王（系列）/指环王2/movie2.iso",
	}
	got := repairCollectionSourceRoot(paths, root, true)
	if got != sourceRoot {
		t.Fatalf("expected %q, got %q", sourceRoot, got)
	}
	values := repairCollectionValues(models.CollectionMetadata{TMDBID: 119, Name: "指环王（系列）"}, got, root, true)
	if values["category"] != "欧美电影" {
		t.Fatalf("expected category, got %#v", values)
	}
}
