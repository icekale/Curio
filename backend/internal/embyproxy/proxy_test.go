package embyproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"curio/internal/mediainfo"
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

func TestResumeRouteUsesPlainItemMediaSourceRewrite(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8096/Users/u/Items/Resume?Recursive=true&MediaTypes=Video", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}
	if !shouldRewriteItemMediaSources("/Users/u/Items/Resume", resp) {
		t.Fatal("expected resume route to keep media-source rewriting")
	}
	if !shouldRewriteResumeItems("/Users/u/Items/Resume", resp) {
		t.Fatal("expected resume route to use resume sanitizer")
	}
	if !shouldNoStorePlaybackResponse("/Users/u/Items/Resume", resp) {
		t.Fatal("expected resume route to be marked no-store")
	}
}

func TestShowEpisodeRoutesUseItemMediaSourceRewrite(t *testing.T) {
	for _, raw := range []string{
		"/Shows/58526/Episodes",
		"/Shows/NextUp",
	} {
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8096"+raw+"?UserId=u&Fields=MediaSources", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    req,
		}
		if !shouldRewriteItemMediaSources(raw, resp) {
			t.Fatalf("expected %s to rewrite media sources", raw)
		}
	}
}

func TestSanitizeHillsResumeLikeItemsRemovesZeroDatePlayedItems(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8096/Users/u/Items?SortBy=DatePlayed&Limit=20", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Client", "Hills")
	payload := map[string]any{
		"Items": []any{
			map[string]any{"Id": "zero", "Type": "Episode", "UserData": map[string]any{"PlaybackPositionTicks": 0, "Played": false}},
			map[string]any{"Id": "resume", "Type": "Episode", "UserData": map[string]any{"PlaybackPositionTicks": 120 * embyTickPerSecond, "Played": false}},
		},
		"TotalRecordCount": float64(2),
	}
	changed, removed := sanitizeHillsResumeLikeItems(req, payload)
	if !changed || removed != 1 {
		t.Fatalf("expected one invalid Hills item to be removed, changed=%t removed=%d", changed, removed)
	}
	items := payload["Items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["Id"] != "resume" {
		t.Fatalf("unexpected items %#v", items)
	}
	if got, _ := int64FromAny(payload["TotalRecordCount"]); got != 1 {
		t.Fatalf("unexpected total %d", got)
	}
}

func TestSanitizeHillsResumeLikeItemsIgnoresNormalEpisodeLists(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8096/Shows/58526/Episodes?UserId=u", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Client", "Hills")
	payload := map[string]any{
		"Items": []any{
			map[string]any{"Id": "episode", "Type": "Episode", "UserData": map[string]any{"PlaybackPositionTicks": 0, "Played": false}},
		},
		"TotalRecordCount": float64(1),
	}
	changed, removed := sanitizeHillsResumeLikeItems(req, payload)
	if changed || removed != 0 {
		t.Fatalf("normal episode lists must not be sanitized, changed=%t removed=%d", changed, removed)
	}
}

func TestRewriteItemMediaSourcesNoMatchSetsNoStore(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8096/Users/u/Items?Recursive=true&SortBy=DatePlayed", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "Vary": []string{"Accept-Encoding"}},
		Body:       io.NopCloser(strings.NewReader(`{"Items":[{"Id":"1","UserData":{"PlaybackPositionTicks":0,"Played":false}}],"TotalRecordCount":1}`)),
		Request:    req,
	}
	if err := (*Proxy)(nil).rewriteItemMediaSources(resp, "/Users/u/Items"); err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") || !strings.Contains(got, "private") {
		t.Fatalf("expected private no-store cache control, got %q", got)
	}
	vary := resp.Header.Get("Vary")
	for _, want := range []string{"Accept-Encoding", "X-Emby-Client", "User-Agent"} {
		if !strings.Contains(vary, want) {
			t.Fatalf("expected Vary to contain %q, got %q", want, vary)
		}
	}
}

func TestFilterInvalidResumeItemsRemovesZeroProgress(t *testing.T) {
	payload := map[string]any{
		"Items": []any{
			map[string]any{"Id": "zero", "UserData": map[string]any{"PlaybackPositionTicks": float64(0), "Played": false}},
			map[string]any{"Id": "played", "UserData": map[string]any{"PlaybackPositionTicks": float64(300 * embyTickPerSecond), "Played": true}},
			map[string]any{"Id": "resume", "UserData": map[string]any{"PlaybackPositionTicks": float64(300 * embyTickPerSecond), "Played": false}},
		},
		"TotalRecordCount": float64(3),
	}
	changed, removed := filterInvalidResumeItems(payload)
	if !changed || removed != 2 {
		t.Fatalf("expected two invalid resume items to be removed, changed=%t removed=%d", changed, removed)
	}
	items := payload["Items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["Id"] != "resume" {
		t.Fatalf("unexpected resume items %#v", items)
	}
	if got, _ := int64FromAny(payload["TotalRecordCount"]); got != 1 {
		t.Fatalf("unexpected total %d", got)
	}
}

func TestMergePlaybackProgressIntoResumePayloadAddsMissingAndSorts(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{
		"Items": []any{
			map[string]any{"Id": "old", "UserData": map[string]any{"PlaybackPositionTicks": float64(50 * embyTickPerSecond), "Played": false}},
		},
		"TotalRecordCount": float64(1),
	}
	progresses := []models.EmbyPlaybackProgress{
		{EmbyItemID: "missing", PositionTicks: 300 * embyTickPerSecond, DurationTicks: 1000 * embyTickPerSecond, UpdatedAt: now.Add(time.Minute)},
		{EmbyItemID: "old", PositionTicks: 120 * embyTickPerSecond, DurationTicks: 900 * embyTickPerSecond, UpdatedAt: now},
	}
	details := map[string]map[string]any{
		"missing": {"Id": "missing", "UserData": map[string]any{"Played": true}, "MediaSources": []any{map[string]any{}}},
	}
	changed, added, updated := mergePlaybackProgressIntoResumePayload(payload, progresses, details, 0)
	if !changed || added != 1 || updated != 1 {
		t.Fatalf("unexpected merge result changed=%t added=%d updated=%d", changed, added, updated)
	}
	items := payload["Items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected two resume items, got %#v", items)
	}
	first := items[0].(map[string]any)
	if first["Id"] != "missing" {
		t.Fatalf("expected newest ledger item first, got %#v", items)
	}
	firstUserData := first["UserData"].(map[string]any)
	if got, _ := int64FromAny(firstUserData["PlaybackPositionTicks"]); got != 300*embyTickPerSecond {
		t.Fatalf("unexpected missing progress %d", got)
	}
	if played, _ := boolFromAny(firstUserData["Played"]); played {
		t.Fatal("missing item should be forced to unplayed")
	}
	if got, _ := int64FromAny(first["RunTimeTicks"]); got != 1000*embyTickPerSecond {
		t.Fatalf("unexpected runtime %d", got)
	}
	second := items[1].(map[string]any)
	secondUserData := second["UserData"].(map[string]any)
	if got, _ := int64FromAny(secondUserData["PlaybackPositionTicks"]); got != 120*embyTickPerSecond {
		t.Fatalf("expected existing item progress to be refreshed, got %d", got)
	}
	if got, _ := int64FromAny(payload["TotalRecordCount"]); got != 2 {
		t.Fatalf("unexpected total %d", got)
	}
}

func TestMergePlaybackProgressIntoResumePayloadRespectsLimitAndCleared(t *testing.T) {
	clearedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	payload := map[string]any{"Items": []any{}}
	progresses := []models.EmbyPlaybackProgress{
		{EmbyItemID: "a", PositionTicks: 200 * embyTickPerSecond, UpdatedAt: clearedAt.Add(2 * time.Minute)},
		{EmbyItemID: "b", PositionTicks: 190 * embyTickPerSecond, UpdatedAt: clearedAt.Add(time.Minute)},
		{EmbyItemID: "cleared", PositionTicks: 180 * embyTickPerSecond, UpdatedAt: clearedAt, ClearedAt: &clearedAt},
	}
	details := map[string]map[string]any{
		"a":       {"Id": "a"},
		"b":       {"Id": "b"},
		"cleared": {"Id": "cleared"},
	}
	changed, added, _ := mergePlaybackProgressIntoResumePayload(payload, progresses, details, 1)
	if !changed || added != 2 {
		t.Fatalf("unexpected merge result changed=%t added=%d", changed, added)
	}
	items := payload["Items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["Id"] != "a" {
		t.Fatalf("expected newest active item only, got %#v", items)
	}
	if got, _ := int64FromAny(payload["TotalRecordCount"]); got != 1 {
		t.Fatalf("unexpected total %d", got)
	}
}

func TestResumeRequestAllowsVideoRespectsMediaTypes(t *testing.T) {
	audioReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/Users/u/Items/Resume?MediaTypes=Audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumeRequestAllowsVideo(audioReq) {
		t.Fatal("audio-only resume requests should not receive video ledger items")
	}
	videoReq, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/Users/u/Items/Resume?MediaTypes=Audio,Video", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resumeRequestAllowsVideo(videoReq) {
		t.Fatal("video resume requests should receive video ledger items")
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
	streams := []any{map[string]any{"Index": 0, "Type": "Video", "Codec": "hevc"}}
	applyDirectPlayMediaSource(source, req, "/Videos/123/stream.mkv?MediaSourceId=ms-1&Static=true", models.STRMLink{RelativePath: "movies/Dunkirk.mkv", Size: 123456789, MediaDurationTicks: 72000000000}, false, streams)
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
	if got := source["RunTimeTicks"]; got != int64(72000000000) {
		t.Fatalf("unexpected RunTimeTicks %#v", got)
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
	gotStreams, ok := source["MediaStreams"].([]any)
	if !ok || len(gotStreams) != 1 {
		t.Fatalf("expected probed media streams, got %#v", source["MediaStreams"])
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
	applyDirectPlayMediaSource(source, req, "/Videos/123/stream.mp4?Static=true", models.STRMLink{RelativePath: "tv/E01.mp4", Size: 987654321}, true, nil)
	if got := source["Path"]; got != "http://192.168.10.83:8097/Videos/123/stream.mp4?Static=true" {
		t.Fatalf("unexpected Path %#v", got)
	}
	if got := source["Container"]; got != "mp4" {
		t.Fatalf("unexpected Container %#v", got)
	}
	if got := source["Size"]; got != int64(987654321) {
		t.Fatalf("unexpected Size %#v", got)
	}
	if got := source["SupportsProbing"]; got != false {
		t.Fatalf("unexpected SupportsProbing %#v", got)
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

func TestEmbyMediaStreamsFromProbePreservesAudioAndSubtitles(t *testing.T) {
	streams := embyMediaStreamsFromProbe([]mediainfo.Stream{
		{Index: 0, Type: "video", Codec: "hevc", Width: 3840, Height: 2160, VideoRange: "HDR10"},
		{Index: 1, Type: "audio", Codec: "truehd", Channels: 8, ChannelLayout: "7.1", Language: "eng", Default: true},
		{Index: 2, Type: "audio", Codec: "dts", Profile: "DTS-HD MA", Channels: 6, ChannelLayout: "5.1", Language: "jpn"},
		{Index: 3, Type: "subtitle", Codec: "pgssub", Language: "chi", Forced: true},
	}, models.STRMLink{RelativePath: "movies/Example.iso"})
	if len(streams) != 4 {
		t.Fatalf("expected all streams, got %#v", streams)
	}
	if index, ok := defaultStreamIndex(streams, "Audio"); !ok || index != 1 {
		t.Fatalf("unexpected default audio index %d ok=%t", index, ok)
	}
	video, _ := streams[0].(map[string]any)
	if video["Width"] != 3840 || video["Height"] != 2160 || video["VideoRange"] != "HDR10" {
		t.Fatalf("unexpected video stream %#v", video)
	}
	subtitle, _ := streams[3].(map[string]any)
	if subtitle["Type"] != "Subtitle" || subtitle["Codec"] != "pgs" || subtitle["IsForced"] != true {
		t.Fatalf("unexpected subtitle stream %#v", subtitle)
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

func TestMediaStreamsForLinkKeepsKnownDurationWithoutSyncProbe(t *testing.T) {
	proxy := New(nil, nil)
	req, err := http.NewRequest(http.MethodGet, "http://192.168.10.83:8097/Items/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	streams, duration := proxy.mediaStreamsForLink(context.Background(), req, models.STRMLink{
		ID:                 "link-1",
		RelativePath:       "shows/E01.mkv",
		MediaDurationTicks: 12345,
	}, true, "UA")
	if len(streams) != 0 {
		t.Fatalf("expected no uncached streams, got %#v", streams)
	}
	if duration != 12345 {
		t.Fatalf("expected cached duration, got %d", duration)
	}
}

func TestMarkMediaProbeScheduledDedupesRecentRequests(t *testing.T) {
	proxy := New(nil, nil)
	if !proxy.markMediaProbeScheduled("link-1\x00UA") {
		t.Fatal("expected first media probe to be scheduled")
	}
	if proxy.markMediaProbeScheduled("link-1\x00UA") {
		t.Fatal("expected duplicate media probe to be skipped")
	}
	proxy.mediaProbeRecent["link-1\x00UA"] = time.Now().Add(-mediaProbeDedupeWindow - time.Second)
	if !proxy.markMediaProbeScheduled("link-1\x00UA") {
		t.Fatal("expected expired media probe key to be scheduled again")
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

func TestPlaybackCheckinRouteSupportsSessionAndLegacyRoutes(t *testing.T) {
	cases := []struct {
		method string
		raw    string
		kind   playbackCheckinKind
		userID string
		itemID string
		ok     bool
	}{
		{http.MethodPost, "/Sessions/Playing", playbackCheckinPlaying, "", "", true},
		{http.MethodPost, "/Sessions/Playing/Progress", playbackCheckinProgress, "", "", true},
		{http.MethodPost, "/Sessions/Playing/Stopped", playbackCheckinStopped, "", "", true},
		{http.MethodPost, "/Users/user-1/PlayingItems/21642/Progress", playbackCheckinProgress, "user-1", "21642", true},
		{http.MethodDelete, "/Users/user-1/PlayingItems/21642", playbackCheckinStopped, "user-1", "21642", true},
		{http.MethodGet, "/Items/21642", "", "", "", false},
	}
	for _, tc := range cases {
		got, ok := playbackCheckinRoute(tc.method, tc.raw)
		if ok != tc.ok {
			t.Fatalf("playbackCheckinRoute(%q) ok=%t want %t", tc.raw, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if got.Kind != tc.kind || got.UserID != tc.userID || got.ItemID != tc.itemID {
			t.Fatalf("playbackCheckinRoute(%q) = %#v", tc.raw, got)
		}
	}
}

func TestManualPlaybackUpdateRouteDetectsResumeClears(t *testing.T) {
	cases := []struct {
		method string
		raw    string
		body   string
		want   bool
	}{
		{http.MethodDelete, "/Users/user-1/PlayedItems/21642", "", true},
		{http.MethodPost, "/Users/user-1/PlayedItems/21642/Delete", "", true},
		{http.MethodPost, "/Users/user-1/PlayedItems/21642", "", false},
		{http.MethodPost, "/Users/user-1/Items/21642/UserData", `{"Played":false}`, true},
		{http.MethodPost, "/Users/user-1/Items/21642/UserData", `{"PlaybackPositionTicks":0,"Played":false}`, true},
		{http.MethodPost, "/Users/user-1/Items/21642/UserData", `{"PlaybackPositionTicks":1200000000,"Played":false}`, false},
	}
	for _, tc := range cases {
		route, ok := manualPlaybackUpdateRoute(tc.method, tc.raw)
		if tc.want && !ok {
			t.Fatalf("manualPlaybackUpdateRoute(%q, %q) did not match", tc.method, tc.raw)
		}
		got := ok && manualPlaybackUpdateClearsResume(route, tc.method, []byte(tc.body))
		if got != tc.want {
			t.Fatalf("manual clear %s %s = %t, want %t", tc.method, tc.raw, got, tc.want)
		}
	}
}

func TestParsePlaybackCheckinReadsBodyQueryAndAuthorization(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8097/Sessions/Playing/Progress?PlaySessionId=query-session", strings.NewReader(`{"ItemId":"21642","MediaSourceId":"ms-1","PositionTicks":123000000,"CanSeek":false}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Yamby", UserId="user-1", Token="token"`)
	route := playbackCheckinRouteInfo{Kind: playbackCheckinProgress}
	body := []byte(`{"ItemId":"21642","MediaSourceId":"ms-1","PositionTicks":123000000,"CanSeek":false}`)
	got := parsePlaybackCheckin(req, route, body)
	if got.ItemID != "21642" || got.UserID != "user-1" || got.MediaSourceID != "ms-1" || got.PlaySessionID != "query-session" {
		t.Fatalf("unexpected checkin %#v", got)
	}
	if !got.HasPositionTicks || got.PositionTicks != 123000000 {
		t.Fatalf("unexpected position %#v", got)
	}
	if !got.HasCanSeek || got.CanSeek {
		t.Fatalf("unexpected can seek %#v", got)
	}
}

func TestRewritePlaybackCheckinBodyPositionUsesLastValidPosition(t *testing.T) {
	body := []byte(`{"ItemId":"21642","PositionTicks":-10000000}`)
	checkin := playbackCheckin{Kind: playbackCheckinStopped, ItemID: "21642", PositionTicks: -10000000, HasPositionTicks: true}
	state := playbackSessionState{LastPositionTicks: 700000000}
	next, changed := rewritePlaybackCheckinBodyPosition(body, checkin, state)
	if !changed {
		t.Fatal("expected body to be patched")
	}
	var payload map[string]any
	if err := json.Unmarshal(next, &payload); err != nil {
		t.Fatal(err)
	}
	if got := int64(payload["PositionTicks"].(float64)); got != 700000000 {
		t.Fatalf("unexpected patched position %d", got)
	}
}

func TestPlaybackCorrectionDecisions(t *testing.T) {
	duration := int64(1000 * embyTickPerSecond)
	cases := []struct {
		name     string
		checkin  playbackCheckin
		state    playbackSessionState
		want     playbackCorrectionAction
		wantPos  int64
		wantText string
	}{
		{
			name:     "invalid stop clears watched",
			checkin:  playbackCheckin{Kind: playbackCheckinStopped, PositionTicks: -10000000, HasPositionTicks: true},
			state:    playbackSessionState{RunTimeTicks: duration},
			want:     playbackCorrectionClearWatched,
			wantPos:  0,
			wantText: "invalid",
		},
		{
			name:     "short playback clears watched",
			checkin:  playbackCheckin{Kind: playbackCheckinStopped, PositionTicks: 30 * embyTickPerSecond, HasPositionTicks: true},
			state:    playbackSessionState{RunTimeTicks: duration},
			want:     playbackCorrectionClearWatched,
			wantPos:  30 * embyTickPerSecond,
			wantText: "threshold",
		},
		{
			name:     "middle playback saves resume",
			checkin:  playbackCheckin{Kind: playbackCheckinStopped, PositionTicks: 200 * embyTickPerSecond, HasPositionTicks: true},
			state:    playbackSessionState{RunTimeTicks: duration},
			want:     playbackCorrectionSaveResume,
			wantPos:  200 * embyTickPerSecond,
			wantText: "watched",
		},
		{
			name:     "near end lets emby decide watched",
			checkin:  playbackCheckin{Kind: playbackCheckinStopped, PositionTicks: 920 * embyTickPerSecond, HasPositionTicks: true},
			state:    playbackSessionState{RunTimeTicks: duration},
			want:     playbackCorrectionNone,
			wantPos:  920 * embyTickPerSecond,
			wantText: "watched",
		},
	}
	for _, tc := range cases {
		got := playbackCorrection(tc.checkin, tc.state)
		if got.Action != tc.want || got.PositionTicks != tc.wantPos || !strings.Contains(got.Reason, tc.wantText) {
			t.Fatalf("%s: got %#v", tc.name, got)
		}
	}
}

func TestPlaybackProgressSaveDecisionUsesCurrentPosition(t *testing.T) {
	state := playbackSessionState{
		RunTimeTicks:      1000 * embyTickPerSecond,
		LastPositionTicks: 120 * embyTickPerSecond,
		MaxPositionTicks:  600 * embyTickPerSecond,
	}
	got := playbackProgressSaveDecision(playbackCheckin{
		Kind:             playbackCheckinProgress,
		PositionTicks:    120 * embyTickPerSecond,
		HasPositionTicks: true,
	}, state)
	if got.Action != playbackCorrectionSaveResume {
		t.Fatalf("expected progress to save resume, got %#v", got)
	}
	if got.PositionTicks != 120*embyTickPerSecond {
		t.Fatalf("expected current position, got %d", got.PositionTicks)
	}
}

func TestFreshStartPlaybackDetectsAndSuppressesStaleResume(t *testing.T) {
	state := playbackSessionState{
		InitialPositionTicks: 350 * embyTickPerSecond,
		HasInitialPosition:   true,
		RunTimeTicks:         1000 * embyTickPerSecond,
	}
	if !playbackStartedFromBeginning(playbackCheckin{
		Kind:             playbackCheckinPlaying,
		PositionTicks:    0,
		HasPositionTicks: true,
	}, state) {
		t.Fatal("expected zero-position playing checkin with existing resume to be treated as fresh start")
	}
	state.FreshStartCleared = true
	state.FreshStartResumeTicks = 350 * embyTickPerSecond
	got := playbackProgressSaveDecision(playbackCheckin{
		Kind:             playbackCheckinProgress,
		PositionTicks:    351 * embyTickPerSecond,
		HasPositionTicks: true,
	}, state)
	if got.Action != playbackCorrectionNone || !strings.Contains(got.Reason, "fresh start") {
		t.Fatalf("expected stale resume progress to be suppressed, got %#v", got)
	}
	got = playbackProgressSaveDecision(playbackCheckin{
		Kind:             playbackCheckinProgress,
		PositionTicks:    20 * embyTickPerSecond,
		HasPositionTicks: true,
	}, state)
	if got.Action != playbackCorrectionSaveResume {
		t.Fatalf("expected real fresh-start progress to save, got %#v", got)
	}
}

func TestPlaybackCorrectionUsesCurrentStopPosition(t *testing.T) {
	state := playbackSessionState{
		RunTimeTicks:      1000 * embyTickPerSecond,
		LastPositionTicks: 120 * embyTickPerSecond,
		MaxPositionTicks:  600 * embyTickPerSecond,
	}
	got := playbackCorrection(playbackCheckin{
		Kind:             playbackCheckinStopped,
		PositionTicks:    30 * embyTickPerSecond,
		HasPositionTicks: true,
	}, state)
	if got.Action != playbackCorrectionClearWatched {
		t.Fatalf("expected short current stop to clear watched, got %#v", got)
	}
	if got.PositionTicks != 30*embyTickPerSecond {
		t.Fatalf("expected current stop position, got %d", got.PositionTicks)
	}
}

func TestPlaybackCorrectionClearsFreshStartStaleStop(t *testing.T) {
	state := playbackSessionState{
		RunTimeTicks:          1000 * embyTickPerSecond,
		FreshStartCleared:     true,
		FreshStartResumeTicks: 350 * embyTickPerSecond,
	}
	got := playbackCorrection(playbackCheckin{
		Kind:             playbackCheckinStopped,
		PositionTicks:    350 * embyTickPerSecond,
		HasPositionTicks: true,
	}, state)
	if got.Action != playbackCorrectionClearWatched || !strings.Contains(got.Reason, "fresh start") {
		t.Fatalf("expected stale fresh-start stop to clear resume, got %#v", got)
	}
}

func TestManualPlaybackClearSuppressesOlderSessionSave(t *testing.T) {
	proxy := New(nil, nil)
	startedAt := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	proxy.rememberManualPlaybackClear("user-1", "item-1", startedAt.Add(time.Minute))
	if !proxy.playbackManuallyCleared([]string{"user-1"}, "item-1", playbackSessionState{StartedAt: startedAt}, startedAt.Add(2*time.Minute)) {
		t.Fatal("expected older playback session to be suppressed")
	}
	if proxy.playbackManuallyCleared([]string{"user-1"}, "item-1", playbackSessionState{StartedAt: startedAt.Add(2 * time.Minute)}, startedAt.Add(3*time.Minute)) {
		t.Fatal("expected newer playback session to be allowed")
	}
}

func TestPlaybackProgressSaveDecisionSkipsNearWatchedThreshold(t *testing.T) {
	got := playbackProgressSaveDecision(playbackCheckin{
		Kind:             playbackCheckinProgress,
		PositionTicks:    920 * embyTickPerSecond,
		HasPositionTicks: true,
	}, playbackSessionState{RunTimeTicks: 1000 * embyTickPerSecond})
	if got.Action != playbackCorrectionNone || !strings.Contains(got.Reason, "watched") {
		t.Fatalf("expected near-end progress to be skipped, got %#v", got)
	}
}

func TestMergePlaybackStatePreservesInitialPlayed(t *testing.T) {
	existing := playbackSessionState{
		UserID:           "user-1",
		ItemID:           "item-1",
		StartedAt:        time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC),
		InitialPlayed:    true,
		HasInitialPlayed: true,
	}
	next := playbackSessionState{
		UserID:    "user-1",
		ItemID:    "item-1",
		StartedAt: time.Date(2026, 5, 19, 12, 0, 10, 0, time.UTC),
	}
	got := mergePlaybackState(existing, next)
	if !got.HasInitialPlayed || !got.InitialPlayed {
		t.Fatalf("expected initial played state to be preserved, got %#v", got)
	}
	if !got.StartedAt.Equal(existing.StartedAt) {
		t.Fatalf("expected earliest start time, got %s", got.StartedAt)
	}
}

func TestMarkPlaybackProgressSavedThrottlesDuplicates(t *testing.T) {
	proxy := New(nil, nil)
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	if !proxy.markPlaybackProgressSaved("user-1", "item-1", 120*embyTickPerSecond, now) {
		t.Fatal("expected first progress save to be allowed")
	}
	if proxy.markPlaybackProgressSaved("user-1", "item-1", 125*embyTickPerSecond, now.Add(5*time.Second)) {
		t.Fatal("expected small duplicate progress to be throttled")
	}
	if !proxy.markPlaybackProgressSaved("user-1", "item-1", 180*embyTickPerSecond, now.Add(6*time.Second)) {
		t.Fatal("expected seek jump to be saved")
	}
	if !proxy.markPlaybackProgressSaved("user-1", "item-1", 185*embyTickPerSecond, now.Add(progressSaveInterval+7*time.Second)) {
		t.Fatal("expected progress after interval to be saved")
	}
}

func TestEmbyAPIURLPreservesQuery(t *testing.T) {
	got, err := embyAPIURL(models.P115Settings{EmbyUpstreamURL: "http://emby:8096/base"}, "/Users/u/Items?SeriesId=s&Limit=20")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/base/Users/u/Items" {
		t.Fatalf("unexpected path %q", got.Path)
	}
	if got.Query().Get("SeriesId") != "s" || got.Query().Get("Limit") != "20" {
		t.Fatalf("unexpected query %q", got.RawQuery)
	}
}

func TestPlaybackSessionStateMergesAliases(t *testing.T) {
	proxy := New(nil, nil)
	first := playbackCheckin{
		Kind:             playbackCheckinProgress,
		UserID:           "user-1",
		ItemID:           "21642",
		PlaySessionID:    "session-1",
		PositionTicks:    120 * embyTickPerSecond,
		HasPositionTicks: true,
	}
	state := proxy.rememberPlaybackCheckin(first, models.STRMLink{ID: "link-1", MediaDurationTicks: 1000 * embyTickPerSecond})
	if state.LastPositionTicks != 120*embyTickPerSecond {
		t.Fatalf("unexpected state %#v", state)
	}
	merged := proxy.mergePlaybackCheckinState(playbackCheckin{Kind: playbackCheckinStopped, PlaySessionID: "session-1"})
	if merged.UserID != "user-1" || merged.ItemID != "21642" || merged.RunTimeTicks != 1000*embyTickPerSecond {
		t.Fatalf("unexpected merged checkin %#v", merged)
	}
}

func TestPlaybackRequestContextPrefersRealEmbyUser(t *testing.T) {
	proxy := New(nil, nil)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8097/Users/d045e0c1b36c4130bf193d19ad79ea14/Items/58619", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy.rememberPlaybackRequestContext(req, "58619", models.STRMLink{ID: "link-1", MediaDurationTicks: 1000 * embyTickPerSecond})
	merged := proxy.mergePlaybackCheckinState(playbackCheckin{
		Kind:          playbackCheckinStopped,
		UserID:        "92f4b4ca-8faa-427a-b803-54afae565ab8",
		ItemID:        "58619",
		PlaySessionID: "session-1",
	})
	if merged.UserID != "d045e0c1b36c4130bf193d19ad79ea14" {
		t.Fatalf("expected trusted request user, got %#v", merged)
	}
	candidates := playbackCorrectionUserIDs(merged, proxy.playbackSessions["item:58619"])
	if len(candidates) == 0 || candidates[0] != "d045e0c1b36c4130bf193d19ad79ea14" {
		t.Fatalf("unexpected correction candidates %#v", candidates)
	}
}

func TestPlaybackRequestUserIDReadsQueryPathAndAuthorization(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8097/Items/123?UserId=query-user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := playbackRequestUserID(req); got != "query-user" {
		t.Fatalf("unexpected query user %q", got)
	}
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8097/Users/path-user/Items/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := playbackRequestUserID(req); got != "path-user" {
		t.Fatalf("unexpected path user %q", got)
	}
	req, err = http.NewRequest(http.MethodGet, "http://127.0.0.1:8097/Items/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Yamby", UserId="auth-user", Token="token"`)
	if got := playbackRequestUserID(req); got != "auth-user" {
		t.Fatalf("unexpected auth user %q", got)
	}
}
