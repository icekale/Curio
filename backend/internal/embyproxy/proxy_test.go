package embyproxy

import (
	"encoding/json"
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
