package embyproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"curio/internal/models"
	"curio/internal/p115"
	"curio/internal/playdiag"
	"curio/internal/repository"
)

type Proxy struct {
	store         *repository.Store
	play          *p115.Service
	prewarmMu     sync.Mutex
	prewarmRecent map[string]time.Time
	prewarmSlots  chan struct{}
}

var (
	playbackInfoPattern = regexp.MustCompile(`(?i)^/?Items/([^/]+)/PlaybackInfo/?$`)
	itemPathPattern     = regexp.MustCompile(`(?i)^/?(?:Users/[^/]+/)?Items(?:/[^/]+)?/?$|^/?Users/[^/]+/Items/Latest/?$`)
	streamPattern       = regexp.MustCompile(`(?i)^/?(?:Videos|Audio)/([^/]+)/(?:stream|universal|original)(?:\.[^/]+)?(?:/|$)|^/?(?:Videos|Audio)/([^/]+)/(?:master\.m3u8|hls|main\.m3u8)(?:/|$)`)
	downloadPattern     = regexp.MustCompile(`(?i)^/?Items/([^/]+)/Download(?:/|$)|^/?Videos/([^/]+)/Download(?:/|$)`)
	itemDetailPattern   = regexp.MustCompile(`(?i)^/?(?:Users/[^/]+/)?Items/([^/]+)/?$`)
)

const (
	publicBaseHeader       = "X-Curio-Public-Base"
	proxyBasePathHeader    = "X-Curio-Proxy-Base-Path"
	playbackPrewarmTimeout = 20 * time.Second
	prewarmDedupeWindow    = 30 * time.Second
	prewarmMaxConcurrent   = 2
	adjacentPrewarmLimit   = 1
)

func New(store *repository.Store, play *p115.Service) *Proxy {
	return &Proxy{
		store:         store,
		play:          play,
		prewarmRecent: map[string]time.Time{},
		prewarmSlots:  make(chan struct{}, prewarmMaxConcurrent),
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	settings, err := p.store.P115Settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(settings.EmbyUpstreamURL) == "" {
		http.Error(w, "Emby upstream URL is not configured", http.StatusNotFound)
		return
	}
	if strings.HasPrefix(strings.TrimLeft(r.URL.Path, "/"), "play/115/") {
		p.servePlay115(w, r, settings)
		return
	}
	if itemID := downloadItemID(proxyPath(settings, r.URL.Path)); itemID != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if p.redirectMappedItem(w, r, itemID, "emby-download") {
			return
		}
	}
	itemID := streamItemID(proxyPath(settings, r.URL.Path))
	if itemID != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if p.redirectMappedItem(w, r, itemID, "emby-stream") {
			return
		}
	}
	p.reverseProxy(settings).ServeHTTP(w, r)
}

func (p *Proxy) redirectMappedItem(w http.ResponseWriter, r *http.Request, itemID, kind string) bool {
	started := time.Now()
	if link, err := p.store.STRMLinkByEmbyItem(r.Context(), "default", itemID); err == nil && link.Status == models.STRMStatusGenerated {
		resolveUA := playbackResolveUserAgent(r)
		directURL, err := p.play.ResolvePlayURLFromRoute(r.Context(), "id/"+link.ID, publicBase(r), resolveUA)
		if err == nil {
			w.Header().Set("X-Curio-Playback-UA", resolveUA)
			logPlayRedirect(kind, r, itemID, directURL, resolveUA, "", time.Since(started))
			writeFoundPlayRedirect(w, directURL)
			return true
		}
		logPlayRedirect(kind, r, itemID, "", resolveUA, err.Error(), time.Since(started))
	} else if err != nil {
		logPlayRedirect(kind, r, itemID, "", playbackResolveUserAgent(r), "no mapped STRM item", time.Since(started))
	}
	return false
}

func (p *Proxy) servePlay115(w http.ResponseWriter, r *http.Request, settings models.P115Settings) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	route := tokenFromPlayURL(r.URL.RequestURI())
	if route == "" {
		http.Error(w, "invalid play route", http.StatusBadRequest)
		return
	}
	started := time.Now()
	resolveUA := playbackResolveUserAgent(r)
	directURL, err := p.play.ResolvePlayURLFromRoute(r.Context(), route, publicBase(r), resolveUA)
	if err != nil {
		logPlayRedirect("play-115", r, route, "", resolveUA, err.Error(), time.Since(started))
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("X-Curio-Playback-UA", resolveUA)
	logPlayRedirect("play-115", r, route, directURL, resolveUA, "", time.Since(started))
	writeFoundPlayRedirect(w, directURL)
}

func (p *Proxy) reverseProxy(settings models.P115Settings) http.Handler {
	upstream, _ := url.Parse(settings.EmbyUpstreamURL)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		incomingBase := publicBase(req)
		incomingProxyBase := activeProxyBasePath(settings, req.URL.Path)
		incomingPath := proxyPath(settings, req.URL.Path)
		originalDirector(req)
		req.URL.Path = joinURLPath(upstream.Path, incomingPath)
		req.Host = upstream.Host
		req.Header.Del("Accept-Encoding")
		req.Header.Set(publicBaseHeader, incomingBase)
		req.Header.Set(proxyBasePathHeader, incomingProxyBase)
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		reqPath := proxyPath(settings, resp.Request.URL.Path)
		if isPlaybackInfo(reqPath) {
			return p.rewritePlaybackInfo(resp, settings)
		}
		if shouldRewriteItemMediaSources(reqPath, resp) {
			return p.rewriteItemMediaSources(resp, reqPath)
		}
		return nil
	}
	return proxy
}

func shouldRewriteItemMediaSources(raw string, resp *http.Response) bool {
	if resp == nil || resp.Request == nil {
		return false
	}
	if resp.StatusCode != http.StatusOK || !itemPathPattern.MatchString(strings.TrimLeft(raw, "/")) {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return contentType == "" || strings.Contains(contentType, "json")
}

func (p *Proxy) rewriteItemMediaSources(resp *http.Response, reqPath string) error {
	started := time.Now()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	prewarm := shouldPrewarmItemMediaSources(reqPath)
	changed := p.rewriteMediaSourcesInValue(resp.Request.Context(), resp.Request, payload, "", true, prewarm)
	if !changed {
		playdiag.Printf("curio emby rewrite items no-match path=%q request_ua=%q elapsed_ms=%d body_bytes=%d", resp.Request.URL.RequestURI(), resp.Request.UserAgent(), time.Since(started).Milliseconds(), len(body))
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	playdiag.Printf("curio emby rewrite items changed path=%q request_ua=%q prewarm=%t elapsed_ms=%d body_bytes=%d", resp.Request.URL.RequestURI(), resp.Request.UserAgent(), prewarm, time.Since(started).Milliseconds(), len(body))
	next, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(next))
	resp.ContentLength = int64(len(next))
	resp.Header.Set("Content-Length", strconvI64(resp.ContentLength))
	resp.Header.Set("Content-Type", "application/json; charset=utf-8")
	return nil
}

func shouldPrewarmItemMediaSources(raw string) bool {
	itemID := itemDetailID(raw)
	return itemID != ""
}

func itemDetailID(raw string) string {
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		raw = raw[:cut]
	}
	match := itemDetailPattern.FindStringSubmatch(strings.TrimLeft(raw, "/"))
	if len(match) != 2 {
		return ""
	}
	itemID := strings.TrimSpace(match[1])
	switch strings.ToLower(itemID) {
	case "", "latest", "resume":
		return ""
	default:
		return itemID
	}
}

func (p *Proxy) rewriteMediaSourcesInValue(ctx context.Context, r *http.Request, value any, parentItemID string, rewritePath bool, prewarm bool) bool {
	changed := false
	switch typed := value.(type) {
	case map[string]any:
		itemID := firstNonEmpty(parentItemID, stringFromAny(typed["ItemId"]), stringFromAny(typed["Id"]))
		if sources, ok := typed["MediaSources"].([]any); ok {
			for _, source := range sources {
				mediaSource, ok := source.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := p.rewriteMediaSource(ctx, r, itemID, mediaSource, rewritePath, prewarm); ok {
					changed = true
				}
			}
		}
		for key, child := range typed {
			if key == "MediaSources" {
				continue
			}
			if p.rewriteMediaSourcesInValue(ctx, r, child, itemID, rewritePath, prewarm) {
				changed = true
			}
		}
	case []any:
		for _, child := range typed {
			if p.rewriteMediaSourcesInValue(ctx, r, child, parentItemID, rewritePath, prewarm) {
				changed = true
			}
		}
	}
	return changed
}

func (p *Proxy) rewriteMediaSource(ctx context.Context, r *http.Request, itemID string, mediaSource map[string]any, rewritePath bool, prewarm bool) (models.STRMLink, bool) {
	link, err := p.linkFromMediaSource(ctx, mediaSource)
	if err != nil {
		return models.STRMLink{}, false
	}
	playURL := embyStreamURL(r, itemID, mediaSource, link)
	if playURL == "" {
		return models.STRMLink{}, false
	}
	resolveUA := playbackResolveUserAgent(r)
	container := mediaContainerForLink(link)
	applyDirectPlayMediaSource(mediaSource, r, playURL, link, rewritePath)
	playdiag.Printf("curio emby rewrite source item=%q media_source=%q link=%s path=%q request_ua=%q rewrite_path=%t direct_stream=%q required_ua=%q container=%q size=%d",
		itemID, stringFromAny(mediaSource["Id"]), shortProxyLogValue(link.ID, 16), r.URL.RequestURI(), r.UserAgent(), rewritePath, playURL, resolveUA, container, link.Size)
	if itemID != "" {
		_ = p.store.UpsertEmbySTRMItem(ctx, models.EmbySTRMItem{
			ID:           stableEmbyID("default", itemID),
			EmbyServerID: "default",
			EmbyItemID:   itemID,
			STRMLinkID:   link.ID,
			STRMPath:     link.STRMPath,
			Status:       "active",
		})
	}
	if prewarm && itemID != "" {
		p.prewarmPlayURL(link.ID, publicBase(r), resolveUA)
		p.prewarmAdjacentPlayURLs(ctx, link, publicBase(r), resolveUA)
	}
	return link, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shortProxyLogValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func (p *Proxy) rewritePlaybackInfo(resp *http.Response, settings models.P115Settings) error {
	started := time.Now()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	itemID := playbackItemID(proxyPath(settings, resp.Request.URL.Path))
	sources, ok := payload["MediaSources"].([]any)
	if !ok {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	changed := false
	for _, source := range sources {
		mediaSource, ok := source.(map[string]any)
		if !ok {
			continue
		}
		_, ok = p.rewriteMediaSource(resp.Request.Context(), resp.Request, itemID, mediaSource, true, true)
		changed = ok || changed
	}
	if !changed {
		playdiag.Printf("curio emby rewrite playback no-match item=%q path=%q request_ua=%q sources=%d elapsed_ms=%d body_bytes=%d", itemID, resp.Request.URL.RequestURI(), resp.Request.UserAgent(), len(sources), time.Since(started).Milliseconds(), len(body))
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
	playdiag.Printf("curio emby rewrite playback changed item=%q path=%q request_ua=%q sources=%d elapsed_ms=%d body_bytes=%d", itemID, resp.Request.URL.RequestURI(), resp.Request.UserAgent(), len(sources), time.Since(started).Milliseconds(), len(body))
	next, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(next))
	resp.ContentLength = int64(len(next))
	resp.Header.Set("Content-Length", strconvI64(resp.ContentLength))
	resp.Header.Set("Content-Type", "application/json; charset=utf-8")
	return nil
}

func (p *Proxy) linkFromMediaSource(ctx context.Context, source map[string]any) (models.STRMLink, error) {
	for _, key := range []string{"Path", "path", "DirectStreamUrl", "directStreamUrl"} {
		raw, _ := source[key].(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if token := tokenFromPlayURL(raw); token != "" {
			linkID, err := p.play.LinkIDFromToken(token)
			if err == nil {
				return p.store.STRMLink(ctx, linkID)
			}
			if link, err := p.store.STRMLinkByPlayRoute(ctx, models.STRMProvider115, token, pathCandidates(raw)); err == nil {
				return link, nil
			}
		}
		for _, candidate := range pathCandidates(raw) {
			if link, err := p.store.STRMLinkByPath(ctx, candidate); err == nil {
				return link, nil
			}
		}
	}
	return models.STRMLink{}, errors.New("no STRM link matched")
}

func applyDirectPlayMediaSource(mediaSource map[string]any, r *http.Request, playURL string, link models.STRMLink, rewritePath bool) {
	if rewritePath {
		mediaSource["Path"] = absolutePlaybackURL(r, playURL)
		mediaSource["Protocol"] = "Http"
		mediaSource["IsRemote"] = true
	}
	mediaSource["DirectStreamUrl"] = playURL
	if container := mediaContainerForLink(link); container != "" {
		mediaSource["Container"] = container
	}
	if link.Size > 0 {
		mediaSource["Size"] = link.Size
	}
	removeRequiredHeader(mediaSource, "User-Agent")
	ensureMediaStreams(mediaSource, link)
	mediaSource["SupportsDirectPlay"] = true
	mediaSource["SupportsDirectStream"] = true
	mediaSource["SupportsTranscoding"] = false
	mediaSource["AddApiKeyToDirectStreamUrl"] = false
	mediaSource["SupportsProbing"] = false
	mediaSource["HasMixedProtocols"] = false
	mediaSource["RequiresOpening"] = false
	mediaSource["RequiresClosing"] = false
	mediaSource["RequiresLooping"] = false
	mediaSource["ReadAtNativeFramerate"] = false
	mediaSource["IsInfiniteStream"] = false
	if _, ok := mediaSource["Type"]; !ok {
		mediaSource["Type"] = "Default"
	}
	delete(mediaSource, "TranscodingUrl")
	delete(mediaSource, "TranscodingSubProtocol")
	delete(mediaSource, "TranscodingContainer")
}

func mediaContainerForLink(link models.STRMLink) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(link.RelativePath)), ".")
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(path.Ext(link.RemotePath)), ".")
	}
	if ext == "" {
		ext = strings.TrimPrefix(strings.ToLower(path.Ext(link.STRMPath)), ".")
	}
	switch ext {
	case "mkv", "mp4", "m4v", "mov", "avi", "wmv", "ts", "m2ts", "mts", "iso", "flv", "webm", "mpg", "mpeg":
		return ext
	default:
		return ""
	}
}

func removeRequiredHeader(mediaSource map[string]any, key string) {
	headers, _ := mediaSource["RequiredHttpHeaders"].(map[string]any)
	if headers == nil {
		return
	}
	delete(headers, key)
	if len(headers) == 0 {
		delete(mediaSource, "RequiredHttpHeaders")
		return
	}
	mediaSource["RequiredHttpHeaders"] = headers
}

func ensureMediaStreams(mediaSource map[string]any, link models.STRMLink) {
	if streams, ok := mediaSource["MediaStreams"].([]any); ok && len(streams) > 0 {
		return
	}
	const (
		defaultBitrate      = int64(8_000_000)
		defaultAudioBitrate = int64(192_000)
	)
	if _, ok := mediaSource["Bitrate"]; !ok {
		mediaSource["Bitrate"] = defaultBitrate
	}
	container := mediaContainerForLink(link)
	displayContainer := strings.ToUpper(container)
	if displayContainer == "" {
		displayContainer = "Video"
	}
	videoCodec := "h264"
	audioCodec := "aac"
	mediaSource["MediaStreams"] = []any{
		map[string]any{
			"Index":                  0,
			"Type":                   "Video",
			"Codec":                  videoCodec,
			"CodecTag":               "avc1",
			"Protocol":               "Http",
			"Width":                  1920,
			"Height":                 1080,
			"AspectRatio":            "16:9",
			"AverageFrameRate":       25,
			"RealFrameRate":          25,
			"BitRate":                defaultBitrate - defaultAudioBitrate,
			"DisplayTitle":           displayContainer + " 1080p",
			"IsDefault":              true,
			"IsExternal":             false,
			"IsForced":               false,
			"IsHearingImpaired":      false,
			"IsInterlaced":           false,
			"IsTextSubtitleStream":   false,
			"Language":               "und",
			"SupportsExternalStream": false,
			"VideoRange":             "SDR",
		},
		map[string]any{
			"Index":                  1,
			"Type":                   "Audio",
			"Codec":                  audioCodec,
			"CodecTag":               "mp4a",
			"Protocol":               "Http",
			"Channels":               2,
			"ChannelLayout":          "stereo",
			"SampleRate":             44100,
			"BitRate":                defaultAudioBitrate,
			"DisplayTitle":           "AAC stereo",
			"IsDefault":              true,
			"IsExternal":             false,
			"IsForced":               false,
			"IsHearingImpaired":      false,
			"IsInterlaced":           false,
			"IsTextSubtitleStream":   false,
			"Language":               "und",
			"Profile":                "LC",
			"SupportsExternalStream": false,
		},
	}
	mediaSource["DefaultAudioStreamIndex"] = 1
	if _, ok := mediaSource["DefaultSubtitleStreamIndex"]; !ok {
		mediaSource["DefaultSubtitleStreamIndex"] = -1
	}
}

func (p *Proxy) prewarmPlayURL(linkID, baseURL, requestUserAgent string) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" || p == nil || p.play == nil {
		return
	}
	prewarmKey := linkID + "\x00" + strings.TrimSpace(requestUserAgent)
	if !p.markPrewarmScheduled(prewarmKey) {
		playdiag.Printf("curio play prewarm skipped link=%s request_ua=%q reason=%q", shortProxyLogValue(linkID, 16), requestUserAgent, "duplicate")
		return
	}
	if p.prewarmSlots != nil {
		select {
		case p.prewarmSlots <- struct{}{}:
		default:
			playdiag.Printf("curio play prewarm skipped link=%s request_ua=%q reason=%q", shortProxyLogValue(linkID, 16), requestUserAgent, "busy")
			return
		}
	}
	go func() {
		if p.prewarmSlots != nil {
			defer func() { <-p.prewarmSlots }()
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), playbackPrewarmTimeout)
		defer cancel()
		if _, err := p.play.ResolvePlayURLFromRoute(ctx, "id/"+linkID, baseURL, requestUserAgent); err != nil {
			playdiag.Printf("curio play prewarm failed link=%s request_ua=%q elapsed_ms=%d err=%s", shortProxyLogValue(linkID, 16), requestUserAgent, time.Since(started).Milliseconds(), err.Error())
			return
		}
		playdiag.Printf("curio play prewarm ok link=%s request_ua=%q elapsed_ms=%d", shortProxyLogValue(linkID, 16), requestUserAgent, time.Since(started).Milliseconds())
	}()
}

func (p *Proxy) prewarmAdjacentPlayURLs(ctx context.Context, link models.STRMLink, baseURL, requestUserAgent string) {
	if p == nil || p.store == nil || adjacentPrewarmLimit <= 0 {
		return
	}
	links, err := p.store.NextSTRMLinks(ctx, link, adjacentPrewarmLimit)
	if err != nil || len(links) == 0 {
		return
	}
	for _, next := range links {
		if next.ID == "" || next.ID == link.ID {
			continue
		}
		playdiag.Printf("curio play prewarm adjacent from=%s link=%s request_ua=%q", shortProxyLogValue(link.ID, 16), shortProxyLogValue(next.ID, 16), requestUserAgent)
		p.prewarmPlayURL(next.ID, baseURL, requestUserAgent)
	}
}

func (p *Proxy) markPrewarmScheduled(key string) bool {
	if p.prewarmRecent == nil {
		return true
	}
	now := time.Now()
	p.prewarmMu.Lock()
	defer p.prewarmMu.Unlock()
	for existingKey, scheduledAt := range p.prewarmRecent {
		if now.Sub(scheduledAt) > prewarmDedupeWindow {
			delete(p.prewarmRecent, existingKey)
		}
	}
	if scheduledAt, ok := p.prewarmRecent[key]; ok && now.Sub(scheduledAt) <= prewarmDedupeWindow {
		return false
	}
	p.prewarmRecent[key] = now
	return true
}

func proxyPath(settings models.P115Settings, raw string) string {
	base := configuredProxyBasePath(settings)
	if strings.HasPrefix(raw, base+"/") {
		return strings.TrimPrefix(raw, base)
	}
	if raw == base {
		return "/"
	}
	return raw
}

func isPlaybackInfo(raw string) bool {
	return playbackInfoPattern.MatchString(strings.TrimLeft(raw, "/"))
}

func playbackItemID(raw string) string {
	match := playbackInfoPattern.FindStringSubmatch(strings.TrimLeft(raw, "/"))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func streamItemID(raw string) string {
	match := streamPattern.FindStringSubmatch(strings.TrimLeft(raw, "/"))
	if len(match) <= 1 {
		return ""
	}
	for _, value := range match[1:] {
		if value != "" {
			return value
		}
	}
	return ""
}

func downloadItemID(raw string) string {
	match := downloadPattern.FindStringSubmatch(strings.TrimLeft(raw, "/"))
	if len(match) <= 1 {
		return ""
	}
	for _, value := range match[1:] {
		if value != "" {
			return value
		}
	}
	return ""
}

func embyStreamURL(r *http.Request, itemID string, source map[string]any, link models.STRMLink) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = strings.TrimSpace(stringFromAny(source["ItemId"]))
	}
	if itemID == "" {
		return ""
	}
	streamName := "stream"
	if container := mediaContainerForLink(link); container != "" {
		streamName += "." + container
	}
	playPath := "/Videos/" + url.PathEscape(itemID) + "/" + streamName
	query := url.Values{}
	if sourceID := strings.TrimSpace(stringFromAny(source["Id"])); sourceID != "" {
		query.Set("MediaSourceId", sourceID)
	}
	query.Set("Static", "true")
	query.Set("AutoOpenLiveStream", "false")
	copyPlaybackAuthQuery(r, query)
	if encoded := query.Encode(); encoded != "" {
		return playPath + "?" + encoded
	}
	return playPath
}

func absolutePlaybackURL(r *http.Request, playbackPath string) string {
	playbackPath = strings.TrimSpace(playbackPath)
	if playbackPath == "" {
		return ""
	}
	if parsed, err := url.Parse(playbackPath); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return playbackPath
	}
	return strings.TrimRight(publicBase(r), "/") + "/" + strings.TrimLeft(playbackPath, "/")
}

func copyPlaybackAuthQuery(r *http.Request, target url.Values) {
	if r == nil || r.URL == nil {
		return
	}
	source := r.URL.Query()
	for _, key := range []string{"api_key", "X-Emby-Token", "X-MediaBrowser-Token"} {
		if value := strings.TrimSpace(source.Get(key)); value != "" {
			target.Set(key, value)
		}
	}
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case int:
		return strconvI64(int64(v))
	case int64:
		return strconvI64(v)
	case float64:
		if v == float64(int64(v)) {
			return strconvI64(int64(v))
		}
	}
	return ""
}

func tokenFromPlayURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil {
		if token := strings.TrimSpace(parsed.Query().Get("token")); token != "" {
			return token
		}
		if parsed.Path != "" {
			raw = parsed.Path
		}
	}
	const prefix = "/play/115/"
	if idx := strings.Index(raw, prefix); idx >= 0 {
		token := raw[idx+len(prefix):]
		if cut := strings.IndexAny(token, "?#"); cut >= 0 {
			token = token[:cut]
		}
		return token
	}
	return ""
}

func playRouteName(link models.STRMLink) string {
	if value := strings.Trim(strings.ReplaceAll(link.RelativePath, "\\", "/"), "/"); value != "" {
		return value
	}
	return path.Base(strings.ReplaceAll(link.STRMPath, "\\", "/"))
}

func pathCandidates(raw string) []string {
	values := []string{raw}
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		values = append(values, parsed.Path)
		if unescaped, err := url.PathUnescape(parsed.Path); err == nil {
			values = append(values, unescaped)
		}
	}
	out := make([]string, 0, len(values)*2)
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		for _, candidate := range []string{value, path.Clean(value)} {
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				out = append(out, candidate)
			}
		}
	}
	return out
}

func activeProxyBasePath(settings models.P115Settings, raw string) string {
	base := configuredProxyBasePath(settings)
	if strings.HasPrefix(raw, base+"/") || raw == base {
		return base
	}
	return ""
}

func configuredProxyBasePath(settings models.P115Settings) string {
	base := strings.TrimRight(settings.EmbyProxyBasePath, "/")
	if base == "" {
		base = "/emby"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base
}

func publicBase(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := strings.TrimSpace(r.Header.Get(publicBaseHeader)); value != "" {
		return strings.TrimRight(value, "/")
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

func proxyBasePath(r *http.Request) string {
	if r == nil {
		return ""
	}
	value := strings.TrimRight(strings.TrimSpace(r.Header.Get(proxyBasePathHeader)), "/")
	if value == "/" {
		return ""
	}
	return value
}

func writeFoundPlayRedirect(w http.ResponseWriter, directURL string) {
	writePlayRedirect(w, directURL, http.StatusFound)
}

func writePlayRedirect(w http.ResponseWriter, directURL string, statusCode int) {
	h := w.Header()
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	h.Set("Vary", "User-Agent")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", "Location")
	h.Set("Location", directURL)
	h.Set("X-Curio-Redirect", "115")
	w.WriteHeader(statusCode)
}

func playbackResolveUserAgent(r *http.Request) string {
	if r != nil && strings.TrimSpace(r.UserAgent()) != "" {
		return strings.TrimSpace(r.UserAgent())
	}
	return p115.DefaultUserAgent()
}

func logPlayRedirect(kind string, r *http.Request, route, directURL, resolveUA, errText string, elapsed time.Duration) {
	if r == nil {
		return
	}
	targetHost := ""
	if parsed, err := url.Parse(directURL); err == nil {
		targetHost = parsed.Host
	}
	if errText != "" {
		playdiag.Printf("curio play %s failed method=%s path=%q route=%q request_ua=%q resolve_ua=%q elapsed_ms=%d err=%s", kind, r.Method, r.URL.RequestURI(), route, r.UserAgent(), resolveUA, elapsed.Milliseconds(), errText)
		return
	}
	playdiag.Printf("curio play %s redirect method=%s path=%q route=%q request_ua=%q resolve_ua=%q target_host=%q elapsed_ms=%d", kind, r.Method, r.URL.RequestURI(), route, r.UserAgent(), resolveUA, targetHost, elapsed.Milliseconds())
}

func joinURLPath(basePath, reqPath string) string {
	if basePath == "" || basePath == "/" {
		if strings.HasPrefix(reqPath, "/") {
			return reqPath
		}
		return "/" + reqPath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(reqPath, "/")
}

func stableEmbyID(serverID, itemID string) string {
	sum := sha256.Sum256([]byte(serverID + ":" + itemID))
	return hex.EncodeToString(sum[:])
}

func strconvI64(value int64) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	v := value
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
