package embyproxy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"curio/internal/models"
)

func TestTokenFromPlayURLUsesQueryToken(t *testing.T) {
	got := tokenFromPlayURL("/play/115/%E6%95%A6%E5%88%BB%E5%B0%94%E5%85%8B.iso?token=abc.def")
	if got != "abc.def" {
		t.Fatalf("expected query token, got %q", got)
	}
}

func TestTokenFromPlayURLKeepsLegacyPathToken(t *testing.T) {
	got := tokenFromPlayURL("/play/115/legacy.token")
	if got != "legacy.token" {
		t.Fatalf("expected legacy token, got %q", got)
	}
}

func TestTokenFromPlayURLUsesAbsoluteURLQueryToken(t *testing.T) {
	got := tokenFromPlayURL("http://localhost:8097/play/115/movie.iso?token=abc.def")
	if got != "abc.def" {
		t.Fatalf("expected query token, got %q", got)
	}
}

func TestTokenFromPlayURLKeepsNestedReadableRoute(t *testing.T) {
	got := tokenFromPlayURL("/play/115/collections/movies/Dunkirk%20(2017)/Dunkirk.iso")
	if got == "" || got == "collections" || !strings.Contains(got, "/") {
		t.Fatalf("expected nested route, got %q", got)
	}
}

func TestTokenFromPlayURLKeepsStableIDRoute(t *testing.T) {
	got := tokenFromPlayURL("/play/115/id/abc123/abc123.mkv")
	if got != "id/abc123/abc123.mkv" {
		t.Fatalf("expected stable id route, got %q", got)
	}
}

func TestPlayRouteNameUsesRelativePath(t *testing.T) {
	got := playRouteName(models.STRMLink{
		RelativePath: "collections/movies/Dunkirk (2017)/Dunkirk.iso",
		STRMPath:     "/data/Curio/strm/collections/movies/Dunkirk (2017)/Dunkirk.strm",
	})
	if got != "collections/movies/Dunkirk (2017)/Dunkirk.iso" {
		t.Fatalf("unexpected route %q", got)
	}
}

func TestPublicBaseUsesIncomingRequestHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Items/1/PlaybackInfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := publicBase(req)
	if got != "http://192.168.10.83:8097" {
		t.Fatalf("unexpected public base %q", got)
	}
}

func TestPublicBaseUsesPreservedProxyBase(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://emby:8096/Items/1/PlaybackInfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(publicBaseHeader, "http://192.168.10.83:8097/")
	got := publicBase(req)
	if got != "http://192.168.10.83:8097" {
		t.Fatalf("unexpected public base %q", got)
	}
}

func TestStreamItemIDSupportsEmbyPlaybackRoutes(t *testing.T) {
	for raw, want := range map[string]string{
		"/Videos/123/stream":              "123",
		"/Videos/123/stream.mkv":          "123",
		"/Videos/123/universal":           "123",
		"/Videos/123/original.strm":       "123",
		"/Videos/123/master.m3u8":         "123",
		"/Videos/123/main.m3u8":           "123",
		"/Audio/abc/original.flac":        "abc",
		"/emby/Videos/123/stream":         "",
		"Videos/123/stream/extra-segment": "123",
	} {
		if got := streamItemID(raw); got != want {
			t.Fatalf("streamItemID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestEmbyStreamURLUsesNativeVideoRoute(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Items/123/PlaybackInfo?api_key=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := embyStreamURL(req, "123", map[string]any{"Id": "ms-1"}, models.STRMLink{RelativePath: "movies/Dunkirk.mkv"})
	want := "/Videos/123/stream.mkv?AutoOpenLiveStream=false&MediaSourceId=ms-1&Static=true&api_key=secret"
	if got != want {
		t.Fatalf("unexpected stream url %q", got)
	}
	if strings.Contains(got, "/play/115/") {
		t.Fatalf("stream url should stay on the Emby route, got %q", got)
	}
}

func TestEmbyStreamURLKeepsIncomingProxyBasePath(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8080/emby/Items/123/PlaybackInfo?X-Emby-Token=token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(proxyBasePathHeader, "/emby")
	got := embyStreamURL(req, "123", map[string]any{"Id": "ms-1"}, models.STRMLink{RelativePath: "tv/E01.mp4"})
	want := "/Videos/123/stream.mp4?AutoOpenLiveStream=false&MediaSourceId=ms-1&Static=true&X-Emby-Token=token"
	if got != want {
		t.Fatalf("unexpected stream url %q", got)
	}
}

func TestItemDetailIDOnlyMatchesConcreteItemDetails(t *testing.T) {
	for raw, want := range map[string]string{
		"/Items/123":                                   "123",
		"/Users/user-id/Items/123":                     "123",
		"/Users/user-id/Items/Latest":                  "",
		"/Users/user-id/Items/Resume":                  "",
		"/Users/user-id/Items":                         "",
		"/Users/user-id/Items/123/PlaybackInfo":        "",
		"/Users/user-id/Items?ParentId=123&Limit=20":   "",
		"/Users/user-id/Items/Latest?ParentId=123":     "",
		"/Users/user-id/Items/Resume?MediaTypes=Video": "",
	} {
		if got := itemDetailID(raw); got != want {
			t.Fatalf("itemDetailID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestApplyDirectPlayMediaSourceUsesNativeStreamURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Items/123/PlaybackInfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := map[string]any{
		"Path":                   "http://192.168.10.83:8080/play/115/id/link-1/link-1.mkv",
		"Protocol":               "Http",
		"IsRemote":               true,
		"Container":              "strm",
		"Size":                   int64(0),
		"MediaStreams":           []any{},
		"RequiredHttpHeaders":    map[string]any{"User-Agent": "OldUA"},
		"TranscodingUrl":         "/Videos/123/master.m3u8",
		"TranscodingSubProtocol": "hls",
		"TranscodingContainer":   "ts",
	}
	applyDirectPlayMediaSource(source, req, "/Videos/123/stream.mkv?MediaSourceId=ms-1&Static=true", models.STRMLink{RelativePath: "movies/Dunkirk.mkv", Size: 123456789}, false)
	if got := source["Path"]; got != "http://192.168.10.83:8080/play/115/id/link-1/link-1.mkv" {
		t.Fatalf("unexpected Path %#v", got)
	}
	if got := source["DirectStreamUrl"]; got != "/Videos/123/stream.mkv?MediaSourceId=ms-1&Static=true" {
		t.Fatalf("unexpected DirectStreamUrl %#v", got)
	}
	if got := source["Container"]; got != "mkv" {
		t.Fatalf("unexpected Container %#v", got)
	}
	if got := source["Size"]; got != int64(123456789) {
		t.Fatalf("unexpected Size %#v", got)
	}
	if got := source["Protocol"]; got != "Http" {
		t.Fatalf("unexpected Protocol %#v", got)
	}
	if got := source["IsRemote"]; got != true {
		t.Fatalf("unexpected IsRemote %#v", got)
	}
	if _, ok := source["TranscodingUrl"]; ok {
		t.Fatal("TranscodingUrl should be removed")
	}
	if _, ok := source["RequiredHttpHeaders"]; ok {
		t.Fatalf("expected required headers to be removed, got %#v", source["RequiredHttpHeaders"])
	}
	streams, ok := source["MediaStreams"].([]any)
	if !ok || len(streams) != 2 {
		t.Fatalf("expected fallback media streams, got %#v", source["MediaStreams"])
	}
	if got := source["SupportsProbing"]; got != false {
		t.Fatalf("unexpected SupportsProbing %#v", got)
	}
}

func TestApplyDirectPlayMediaSourceCanRewritePathToProxyURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Items/123/PlaybackInfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	source := map[string]any{"Path": "http://172.16.0.1:8080/play/115/id/link-1/link-1.mkv"}
	applyDirectPlayMediaSource(source, req, "/Videos/123/stream.mp4?Static=true", models.STRMLink{RelativePath: "tv/E01.mp4", Size: 987654321}, true)
	if got := source["Path"]; got != "http://192.168.10.83:8097/Videos/123/stream.mp4?Static=true" {
		t.Fatalf("unexpected Path %#v", got)
	}
	if got := source["Container"]; got != "mp4" {
		t.Fatalf("unexpected Container %#v", got)
	}
	if got := source["Size"]; got != int64(987654321) {
		t.Fatalf("unexpected Size %#v", got)
	}
}

func TestMediaContainerForLinkIgnoresSTRMPathExtension(t *testing.T) {
	got := mediaContainerForLink(models.STRMLink{
		RelativePath: "shows/Dark/S01E01.m2ts",
		STRMPath:     "/data/Curio/strm/shows/Dark/S01E01.strm",
	})
	if got != "m2ts" {
		t.Fatalf("unexpected container %q", got)
	}
}

func TestPlaybackResolveUserAgentInheritsPlayerRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Videos/123/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "Fileball/1.0")
	if got := playbackResolveUserAgent(req); got != "Fileball/1.0" {
		t.Fatalf("expected inherited user agent, got %q", got)
	}
}

func TestMarkPrewarmScheduledDedupesRecentRequests(t *testing.T) {
	proxy := New(nil, nil)
	if !proxy.markPrewarmScheduled("link-1\x00UA") {
		t.Fatal("expected first prewarm to be scheduled")
	}
	if proxy.markPrewarmScheduled("link-1\x00UA") {
		t.Fatal("expected duplicate prewarm to be skipped")
	}
	proxy.prewarmRecent["link-1\x00UA"] = time.Now().Add(-prewarmDedupeWindow - time.Second)
	if !proxy.markPrewarmScheduled("link-1\x00UA") {
		t.Fatal("expected expired prewarm key to be scheduled again")
	}
}

func TestDownloadItemIDSupportsDownloadRoutes(t *testing.T) {
	for raw, want := range map[string]string{
		"/Items/123/Download":  "123",
		"/Videos/abc/Download": "abc",
		"/Items/123":           "",
	} {
		if got := downloadItemID(raw); got != want {
			t.Fatalf("downloadItemID(%q) = %q, want %q", raw, got, want)
		}
	}
}
