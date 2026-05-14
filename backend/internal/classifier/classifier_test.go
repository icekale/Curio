package classifier

import (
	"testing"

	"curio/internal/models"
)

func TestMatchOrderedFallback(t *testing.T) {
	raw := `
movie:
  Documentary:
    genre_ids: "99,-10402"
  Music:
    genre_ids: "10402"
  Other:
`
	category, err := Match(raw, models.MediaMovie, Item{GenreIDs: []string{"99"}, OriginalLanguage: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if category != "Documentary" {
		t.Fatalf("expected Documentary, got %q", category)
	}
	category, err = Match(raw, models.MediaMovie, Item{GenreIDs: []string{"18"}, OriginalLanguage: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if category != "Other" {
		t.Fatalf("expected fallback, got %q", category)
	}
}

func TestMatchTVCountries(t *testing.T) {
	raw := `
tv:
  Anime:
    genre_ids: "16"
    origin_country: "JP"
  Other:
`
	category, err := Match(raw, models.MediaTVEpisode, Item{GenreIDs: []string{"16"}, OriginCountry: []string{"JP"}})
	if err != nil {
		t.Fatal(err)
	}
	if category != "Anime" {
		t.Fatalf("expected Anime, got %q", category)
	}
}

func TestMatchKeywordsSupportsNegativeRules(t *testing.T) {
	raw := `
movie:
  Concert:
    keywords: "concert,-behind the scenes"
  Other:
`
	category, err := Match(raw, models.MediaMovie, Item{Keywords: []string{"concert", "behind the scenes"}})
	if err != nil {
		t.Fatal(err)
	}
	if category != "Other" {
		t.Fatalf("expected negative keyword to exclude Concert, got %q", category)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse(`
movie:
  Broken:
    made_up: "1"
`)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}
