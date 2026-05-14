package organizer

import (
	"strings"
	"testing"

	"curio/internal/models"
)

func TestFailureFileNameUsesStableSuffix(t *testing.T) {
	name := FailureFileName(models.MediaFile{
		ID:           "01234567-89ab-cdef-0123-456789abcdef",
		OriginalName: "Movie.mkv",
		FileHash:     "abcdef1234567890",
	})
	if name != "Movie.abcdef123456.mkv" {
		t.Fatalf("unexpected failure name %q", name)
	}
}

func TestFailureFileNameFallsBackToFileID(t *testing.T) {
	name := FailureFileName(models.MediaFile{
		ID:           "01234567-89ab-cdef-0123-456789abcdef",
		OriginalName: "Movie.mkv",
	})
	if !strings.HasPrefix(name, "Movie.0123456789ab") {
		t.Fatalf("expected file id suffix, got %q", name)
	}
}
