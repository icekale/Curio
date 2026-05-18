package aifilename

import "testing"

func TestDecodeAnalysisPayloadHandlesFencedJSONAndLooseTypes(t *testing.T) {
	raw := "```json\n{\"items\":[{\"index\":\"2\",\"media_type\":\"movie\",\"title\":\"Alien 3\",\"alternative_titles\":[\"Alien³\"],\"year\":\"1992\",\"season\":null,\"episode\":null,\"episode_end\":null,\"resolution\":\"1080p\",\"source\":\"BluRay\",\"video_codec\":\"AVC\",\"audio_codec\":\"DTS-HD MA\",\"audio_channels\":\"5.1\",\"hdr_format\":\"\",\"edition\":\"Theatrical Version / Special Edition / 2in1\",\"release_group\":\"NGB\",\"confidence\":\"0.94\",\"needs_review\":\"false\",\"reason\":\"\"}]}\n```"
	items, err := decodeAnalysisPayload([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0]
	if item.Index != 2 || item.MediaType != "movie" || item.Title != "Alien 3" || item.Year != 1992 {
		t.Fatalf("unexpected item: %+v", item)
	}
	if item.Confidence != 0.94 || item.NeedsReview {
		t.Fatalf("unexpected confidence/review fields: %+v", item)
	}
}

func TestChatCompletionsURLAcceptsEndpointOrBase(t *testing.T) {
	if got := chatCompletionsURL("https://example.com/v1"); got != "https://example.com/v1/chat/completions" {
		t.Fatalf("unexpected base URL result: %s", got)
	}
	if got := chatCompletionsURL("https://example.com/v1/chat/completions"); got != "https://example.com/v1/chat/completions" {
		t.Fatalf("unexpected endpoint URL result: %s", got)
	}
}
