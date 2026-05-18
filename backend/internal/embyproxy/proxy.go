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
	"strconv"
	"strings"
	"sync"
	"time"

	"curio/internal/mediainfo"
	"curio/internal/models"
	"curio/internal/p115"
	"curio/internal/playdiag"
	"curio/internal/repository"
)

type Proxy struct {
	store            *repository.Store
	play             *p115.Service
	prewarmMu        sync.Mutex
	prewarmRecent    map[string]time.Time
	prewarmSlots     chan struct{}
	playbackMu       sync.Mutex
	playbackSessions map[string]playbackSessionState
}

var (
	playbackInfoPattern           = regexp.MustCompile(`(?i)^/?Items/([^/]+)/PlaybackInfo/?$`)
	itemPathPattern               = regexp.MustCompile(`(?i)^/?(?:Users/[^/]+/)?Items(?:/[^/]+)?/?$|^/?Users/[^/]+/Items/Latest/?$`)
	streamPattern                 = regexp.MustCompile(`(?i)^/?(?:Videos|Audio)/([^/]+)/(?:stream|universal|original)(?:\.[^/]+)?(?:/|$)|^/?(?:Videos|Audio)/([^/]+)/(?:master\.m3u8|hls|main\.m3u8)(?:/|$)`)
	downloadPattern               = regexp.MustCompile(`(?i)^/?Items/([^/]+)/Download(?:/|$)|^/?Videos/([^/]+)/Download(?:/|$)`)
	itemDetailPattern             = regexp.MustCompile(`(?i)^/?(?:Users/[^/]+/)?Items/([^/]+)/?$`)
	sessionPlayingPattern         = regexp.MustCompile(`(?i)^/?Sessions/Playing/?$`)
	sessionPlayingProgressPattern = regexp.MustCompile(`(?i)^/?Sessions/Playing/Progress/?$`)
	sessionPlayingStoppedPattern  = regexp.MustCompile(`(?i)^/?Sessions/Playing/Stopped/?$`)
	legacyPlayingItemPattern      = regexp.MustCompile(`(?i)^/?Users/([^/]+)/PlayingItems/([^/]+)/?$`)
	legacyPlayingProgressPattern  = regexp.MustCompile(`(?i)^/?Users/([^/]+)/PlayingItems/([^/]+)/Progress/?$`)
	legacyPlayingStoppedPattern   = regexp.MustCompile(`(?i)^/?Users/([^/]+)/PlayingItems/([^/]+)/(?:Delete|Stopped)/?$`)
)

const (
	publicBaseHeader       = "X-Curio-Public-Base"
	proxyBasePathHeader    = "X-Curio-Proxy-Base-Path"
	playbackPrewarmTimeout = 20 * time.Second
	prewarmDedupeWindow    = 30 * time.Second
	prewarmMaxConcurrent   = 2
	adjacentPrewarmLimit   = 1
	embyTickPerSecond      = int64(10000000)
	playbackShortStopTicks = 60 * embyTickPerSecond
	playbackResumeTicks    = 10 * embyTickPerSecond
	playbackStateTTL       = 6 * time.Hour
	embyCorrectionTimeout  = 8 * time.Second
)

func New(store *repository.Store, play *p115.Service) *Proxy {
	return &Proxy{
		store:            store,
		play:             play,
		prewarmRecent:    map[string]time.Time{},
		prewarmSlots:     make(chan struct{}, prewarmMaxConcurrent),
		playbackSessions: map[string]playbackSessionState{},
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
	if route, ok := playbackCheckinRoute(r.Method, proxyPath(settings, r.URL.Path)); ok {
		p.servePlaybackCheckin(w, r, settings, route)
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

type playbackCheckinKind string

const (
	playbackCheckinPlaying  playbackCheckinKind = "playing"
	playbackCheckinProgress playbackCheckinKind = "progress"
	playbackCheckinStopped  playbackCheckinKind = "stopped"
)

type playbackCheckinRouteInfo struct {
	Kind   playbackCheckinKind
	UserID string
	ItemID string
}

type playbackCheckin struct {
	Kind              playbackCheckinKind
	UserID            string
	ItemID            string
	MediaSourceID     string
	PlaySessionID     string
	PlayMethod        string
	EventName         string
	PositionTicks     int64
	HasPositionTicks  bool
	CanSeek           bool
	HasCanSeek        bool
	RunTimeTicks      int64
	HasRunTimeTicks   bool
	OriginalPath      string
	OriginalUserAgent string
	LinkID            string
}

type playbackSessionState struct {
	UserID            string
	ItemID            string
	MediaSourceID     string
	PlaySessionID     string
	LinkID            string
	RunTimeTicks      int64
	LastPositionTicks int64
	MaxPositionTicks  int64
	UpdatedAt         time.Time
}

type playbackCorrectionAction string

const (
	playbackCorrectionNone         playbackCorrectionAction = "none"
	playbackCorrectionClearWatched playbackCorrectionAction = "clear_watched"
	playbackCorrectionSaveResume   playbackCorrectionAction = "save_resume"
)

type playbackCorrectionDecision struct {
	Action        playbackCorrectionAction
	PositionTicks int64
	RunTimeTicks  int64
	Reason        string
}

type responseStatusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseStatusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseStatusRecorder) Write(body []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (r *responseStatusRecorder) StatusCode() int {
	if r.statusCode == 0 {
		return http.StatusOK
	}
	return r.statusCode
}

func (r *responseStatusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (p *Proxy) servePlaybackCheckin(w http.ResponseWriter, r *http.Request, settings models.P115Settings, route playbackCheckinRouteInfo) {
	var body []byte
	var err error
	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
	}
	if err != nil {
		playdiag.Printf("curio emby playback checkin read failed kind=%s path=%q request_ua=%q err=%s", route.Kind, r.URL.RequestURI(), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Body != nil {
		_ = r.Body.Close()
	}
	checkin := parsePlaybackCheckin(r, route, body)
	checkin = p.mergePlaybackCheckinState(checkin)

	link, mapped := p.playbackLinkForCheckin(r.Context(), checkin)
	if mapped {
		checkin.LinkID = link.ID
		if link.MediaDurationTicks > 0 {
			checkin.RunTimeTicks = link.MediaDurationTicks
			checkin.HasRunTimeTicks = true
		}
		if checkin.UserID == "" && checkin.Kind == playbackCheckinStopped {
			if userID := p.resolveEmbyUserIDFromSessions(r.Context(), settings, r, checkin); userID != "" {
				checkin.UserID = userID
			}
		}
	}

	state := playbackSessionState{}
	if mapped {
		state = p.rememberPlaybackCheckin(checkin, link)
		if nextBody, changed := rewritePlaybackCheckinBodyPosition(body, checkin, state); changed {
			body = nextBody
			checkin.PositionTicks = state.LastPositionTicks
			checkin.HasPositionTicks = true
			playdiag.Printf("curio emby playback checkin patched kind=%s item=%q user=%q session=%q link=%s position_ticks=%d path=%q request_ua=%q",
				checkin.Kind, checkin.ItemID, checkin.UserID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), checkin.PositionTicks, checkin.OriginalPath, checkin.OriginalUserAgent)
		}
	}

	logPlaybackCheckin(checkin, mapped, state)
	restoreRequestBody(r, body)
	recorder := &responseStatusRecorder{ResponseWriter: w}
	p.reverseProxy(settings).ServeHTTP(recorder, r)

	if mapped && checkin.Kind == playbackCheckinStopped {
		p.correctStoppedPlayback(settings, r, checkin, state, recorder.StatusCode())
		p.forgetPlaybackCheckin(checkin, state)
	}
}

func restoreRequestBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	if len(body) > 0 {
		r.Header.Set("Content-Length", strconvI64(int64(len(body))))
	} else {
		r.Header.Del("Content-Length")
	}
}

func playbackCheckinRoute(method, raw string) (playbackCheckinRouteInfo, bool) {
	if cut := strings.IndexAny(raw, "?#"); cut >= 0 {
		raw = raw[:cut]
	}
	cleaned := strings.TrimLeft(raw, "/")
	if strings.EqualFold(method, http.MethodPost) {
		switch {
		case sessionPlayingProgressPattern.MatchString(cleaned):
			return playbackCheckinRouteInfo{Kind: playbackCheckinProgress}, true
		case sessionPlayingStoppedPattern.MatchString(cleaned):
			return playbackCheckinRouteInfo{Kind: playbackCheckinStopped}, true
		case sessionPlayingPattern.MatchString(cleaned):
			return playbackCheckinRouteInfo{Kind: playbackCheckinPlaying}, true
		}
	}
	if match := legacyPlayingProgressPattern.FindStringSubmatch(cleaned); len(match) == 3 && strings.EqualFold(method, http.MethodPost) {
		return playbackCheckinRouteInfo{Kind: playbackCheckinProgress, UserID: unescapePathValue(match[1]), ItemID: unescapePathValue(match[2])}, true
	}
	if match := legacyPlayingStoppedPattern.FindStringSubmatch(cleaned); len(match) == 3 && strings.EqualFold(method, http.MethodPost) {
		return playbackCheckinRouteInfo{Kind: playbackCheckinStopped, UserID: unescapePathValue(match[1]), ItemID: unescapePathValue(match[2])}, true
	}
	if match := legacyPlayingItemPattern.FindStringSubmatch(cleaned); len(match) == 3 {
		if strings.EqualFold(method, http.MethodDelete) {
			return playbackCheckinRouteInfo{Kind: playbackCheckinStopped, UserID: unescapePathValue(match[1]), ItemID: unescapePathValue(match[2])}, true
		}
		if strings.EqualFold(method, http.MethodPost) {
			return playbackCheckinRouteInfo{Kind: playbackCheckinPlaying, UserID: unescapePathValue(match[1]), ItemID: unescapePathValue(match[2])}, true
		}
	}
	return playbackCheckinRouteInfo{}, false
}

func parsePlaybackCheckin(r *http.Request, route playbackCheckinRouteInfo, body []byte) playbackCheckin {
	checkin := playbackCheckin{
		Kind:              route.Kind,
		UserID:            route.UserID,
		ItemID:            route.ItemID,
		OriginalPath:      r.URL.RequestURI(),
		OriginalUserAgent: r.UserAgent(),
	}
	var payload map[string]any
	if len(bytes.TrimSpace(body)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err == nil {
			applyPlaybackPayload(&checkin, payload)
		}
	}
	applyPlaybackQuery(&checkin, r.URL.Query())
	if checkin.UserID == "" {
		checkin.UserID = embyAuthorizationField(r.Header.Get("X-Emby-Authorization"), "UserId")
	}
	if checkin.UserID == "" {
		checkin.UserID = embyAuthorizationField(r.Header.Get("Authorization"), "UserId")
	}
	return checkin
}

func applyPlaybackPayload(checkin *playbackCheckin, payload map[string]any) {
	if checkin.UserID == "" {
		checkin.UserID = firstNonEmpty(stringFromAny(payload["UserId"]), stringFromAny(payload["UserID"]))
	}
	if checkin.ItemID == "" {
		checkin.ItemID = firstNonEmpty(stringFromAny(payload["ItemId"]), stringFromAny(payload["ItemID"]), stringFromAny(payload["Id"]))
	}
	checkin.MediaSourceID = firstNonEmpty(checkin.MediaSourceID, stringFromAny(payload["MediaSourceId"]), stringFromAny(payload["MediaSourceID"]))
	checkin.PlaySessionID = firstNonEmpty(checkin.PlaySessionID, stringFromAny(payload["PlaySessionId"]), stringFromAny(payload["PlaySessionID"]))
	checkin.PlayMethod = firstNonEmpty(checkin.PlayMethod, stringFromAny(payload["PlayMethod"]))
	checkin.EventName = firstNonEmpty(checkin.EventName, stringFromAny(payload["EventName"]))
	if position, ok := int64FromAny(payload["PositionTicks"]); ok {
		checkin.PositionTicks = position
		checkin.HasPositionTicks = true
	}
	if runTime, ok := int64FromAny(payload["RunTimeTicks"]); ok {
		checkin.RunTimeTicks = runTime
		checkin.HasRunTimeTicks = true
	}
	if canSeek, ok := boolFromAny(payload["CanSeek"]); ok {
		checkin.CanSeek = canSeek
		checkin.HasCanSeek = true
	}
	if item, ok := payload["Item"].(map[string]any); ok {
		if checkin.ItemID == "" {
			checkin.ItemID = stringFromAny(item["Id"])
		}
		if runTime, ok := int64FromAny(item["RunTimeTicks"]); ok && !checkin.HasRunTimeTicks {
			checkin.RunTimeTicks = runTime
			checkin.HasRunTimeTicks = true
		}
	}
}

func applyPlaybackQuery(checkin *playbackCheckin, query url.Values) {
	checkin.UserID = firstNonEmpty(checkin.UserID, query.Get("UserId"), query.Get("UserID"), query.Get("userId"))
	checkin.ItemID = firstNonEmpty(checkin.ItemID, query.Get("ItemId"), query.Get("ItemID"), query.Get("itemId"), query.Get("Id"))
	checkin.MediaSourceID = firstNonEmpty(checkin.MediaSourceID, query.Get("MediaSourceId"), query.Get("MediaSourceID"))
	checkin.PlaySessionID = firstNonEmpty(checkin.PlaySessionID, query.Get("PlaySessionId"), query.Get("PlaySessionID"))
	if position, ok := int64FromString(firstNonEmpty(query.Get("PositionTicks"), query.Get("positionTicks"))); ok {
		checkin.PositionTicks = position
		checkin.HasPositionTicks = true
	}
	if canSeek, ok := boolFromString(firstNonEmpty(query.Get("CanSeek"), query.Get("canSeek"))); ok {
		checkin.CanSeek = canSeek
		checkin.HasCanSeek = true
	}
}

func (p *Proxy) playbackLinkForCheckin(ctx context.Context, checkin playbackCheckin) (models.STRMLink, bool) {
	if p == nil || p.store == nil || checkin.ItemID == "" {
		return models.STRMLink{}, false
	}
	link, err := p.store.STRMLinkByEmbyItem(ctx, "default", checkin.ItemID)
	if err != nil || link.Status != models.STRMStatusGenerated {
		return models.STRMLink{}, false
	}
	return link, true
}

func logPlaybackCheckin(checkin playbackCheckin, mapped bool, state playbackSessionState) {
	canSeek := "unknown"
	if checkin.HasCanSeek {
		canSeek = strconv.FormatBool(checkin.CanSeek)
	}
	playdiag.Printf("curio emby playback checkin kind=%s item=%q user=%q session=%q media_source=%q position_ticks=%d has_position=%t can_seek=%s play_method=%q event=%q mapped=%t link=%s duration_ticks=%d path=%q request_ua=%q",
		checkin.Kind, checkin.ItemID, checkin.UserID, checkin.PlaySessionID, checkin.MediaSourceID, checkin.PositionTicks, checkin.HasPositionTicks, canSeek,
		checkin.PlayMethod, checkin.EventName, mapped, shortProxyLogValue(checkin.LinkID, 16), state.RunTimeTicks, checkin.OriginalPath, checkin.OriginalUserAgent)
}

func (p *Proxy) mergePlaybackCheckinState(checkin playbackCheckin) playbackCheckin {
	state, ok := p.playbackState(checkin)
	if !ok {
		return checkin
	}
	checkin.UserID = firstNonEmpty(checkin.UserID, state.UserID)
	checkin.ItemID = firstNonEmpty(checkin.ItemID, state.ItemID)
	checkin.MediaSourceID = firstNonEmpty(checkin.MediaSourceID, state.MediaSourceID)
	checkin.PlaySessionID = firstNonEmpty(checkin.PlaySessionID, state.PlaySessionID)
	checkin.LinkID = firstNonEmpty(checkin.LinkID, state.LinkID)
	if !checkin.HasRunTimeTicks && state.RunTimeTicks > 0 {
		checkin.RunTimeTicks = state.RunTimeTicks
		checkin.HasRunTimeTicks = true
	}
	return checkin
}

func (p *Proxy) playbackState(checkin playbackCheckin) (playbackSessionState, bool) {
	if p == nil {
		return playbackSessionState{}, false
	}
	keys := playbackSessionKeys(checkin.UserID, checkin.ItemID, checkin.PlaySessionID)
	if len(keys) == 0 {
		return playbackSessionState{}, false
	}
	p.playbackMu.Lock()
	defer p.playbackMu.Unlock()
	for _, key := range keys {
		if state, ok := p.playbackSessions[key]; ok {
			return state, true
		}
	}
	return playbackSessionState{}, false
}

func (p *Proxy) rememberPlaybackCheckin(checkin playbackCheckin, link models.STRMLink) playbackSessionState {
	if p == nil {
		return playbackSessionState{}
	}
	now := time.Now()
	p.playbackMu.Lock()
	defer p.playbackMu.Unlock()
	if p.playbackSessions == nil {
		p.playbackSessions = map[string]playbackSessionState{}
	}
	cleanupPlaybackSessionsLocked(p.playbackSessions, now)
	state := playbackSessionState{
		UserID:        checkin.UserID,
		ItemID:        checkin.ItemID,
		MediaSourceID: checkin.MediaSourceID,
		PlaySessionID: checkin.PlaySessionID,
		LinkID:        firstNonEmpty(checkin.LinkID, link.ID),
		RunTimeTicks:  firstPositiveI64(checkin.RunTimeTicks, link.MediaDurationTicks),
		UpdatedAt:     now,
	}
	for _, key := range playbackSessionKeys(checkin.UserID, checkin.ItemID, checkin.PlaySessionID) {
		if existing, ok := p.playbackSessions[key]; ok {
			state = mergePlaybackState(existing, state)
			break
		}
	}
	if checkin.HasPositionTicks && checkin.PositionTicks > 0 {
		state.LastPositionTicks = checkin.PositionTicks
		if checkin.PositionTicks > state.MaxPositionTicks {
			state.MaxPositionTicks = checkin.PositionTicks
		}
	}
	state.UpdatedAt = now
	for _, key := range playbackSessionKeys(state.UserID, state.ItemID, state.PlaySessionID) {
		p.playbackSessions[key] = state
	}
	return state
}

func (p *Proxy) forgetPlaybackCheckin(checkin playbackCheckin, state playbackSessionState) {
	if p == nil {
		return
	}
	p.playbackMu.Lock()
	defer p.playbackMu.Unlock()
	for _, key := range playbackSessionKeys(firstNonEmpty(checkin.UserID, state.UserID), firstNonEmpty(checkin.ItemID, state.ItemID), firstNonEmpty(checkin.PlaySessionID, state.PlaySessionID)) {
		delete(p.playbackSessions, key)
	}
}

func mergePlaybackState(existing, next playbackSessionState) playbackSessionState {
	next.UserID = firstNonEmpty(next.UserID, existing.UserID)
	next.ItemID = firstNonEmpty(next.ItemID, existing.ItemID)
	next.MediaSourceID = firstNonEmpty(next.MediaSourceID, existing.MediaSourceID)
	next.PlaySessionID = firstNonEmpty(next.PlaySessionID, existing.PlaySessionID)
	next.LinkID = firstNonEmpty(next.LinkID, existing.LinkID)
	next.RunTimeTicks = firstPositiveI64(next.RunTimeTicks, existing.RunTimeTicks)
	next.LastPositionTicks = firstPositiveI64(next.LastPositionTicks, existing.LastPositionTicks)
	if existing.MaxPositionTicks > next.MaxPositionTicks {
		next.MaxPositionTicks = existing.MaxPositionTicks
	}
	return next
}

func playbackSessionKeys(userID, itemID, playSessionID string) []string {
	keys := []string{}
	if playSessionID = strings.TrimSpace(playSessionID); playSessionID != "" {
		keys = append(keys, "session:"+playSessionID)
	}
	if userID = strings.TrimSpace(userID); userID != "" && strings.TrimSpace(itemID) != "" {
		keys = append(keys, "user-item:"+userID+":"+strings.TrimSpace(itemID))
	}
	if itemID = strings.TrimSpace(itemID); itemID != "" {
		keys = append(keys, "item:"+itemID)
	}
	return keys
}

func cleanupPlaybackSessionsLocked(sessions map[string]playbackSessionState, now time.Time) {
	for key, state := range sessions {
		if now.Sub(state.UpdatedAt) > playbackStateTTL {
			delete(sessions, key)
		}
	}
}

func rewritePlaybackCheckinBodyPosition(body []byte, checkin playbackCheckin, state playbackSessionState) ([]byte, bool) {
	if len(bytes.TrimSpace(body)) == 0 || state.LastPositionTicks <= 0 {
		return body, false
	}
	if checkin.Kind != playbackCheckinProgress && checkin.Kind != playbackCheckinStopped {
		return body, false
	}
	if checkin.HasPositionTicks && checkin.PositionTicks > 0 {
		return body, false
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return body, false
	}
	payload["PositionTicks"] = state.LastPositionTicks
	next, err := json.Marshal(payload)
	if err != nil {
		return body, false
	}
	return next, true
}

func (p *Proxy) correctStoppedPlayback(settings models.P115Settings, r *http.Request, checkin playbackCheckin, state playbackSessionState, upstreamStatus int) {
	if upstreamStatus < http.StatusOK || upstreamStatus >= http.StatusBadRequest {
		playdiag.Printf("curio emby playback correction skipped item=%q user=%q session=%q link=%s reason=%q upstream_status=%d path=%q request_ua=%q",
			checkin.ItemID, checkin.UserID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), "upstream did not accept checkin", upstreamStatus, checkin.OriginalPath, checkin.OriginalUserAgent)
		return
	}
	decision := playbackCorrection(checkin, state)
	if decision.Action == playbackCorrectionNone {
		playdiag.Printf("curio emby playback correction skipped item=%q user=%q session=%q link=%s reason=%q position_ticks=%d duration_ticks=%d path=%q request_ua=%q",
			checkin.ItemID, checkin.UserID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), decision.Reason, decision.PositionTicks, decision.RunTimeTicks, checkin.OriginalPath, checkin.OriginalUserAgent)
		return
	}
	userID := firstNonEmpty(checkin.UserID, state.UserID)
	itemID := firstNonEmpty(checkin.ItemID, state.ItemID)
	if userID == "" || itemID == "" {
		playdiag.Printf("curio emby playback correction skipped item=%q user=%q session=%q link=%s action=%s reason=%q path=%q request_ua=%q",
			itemID, userID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), decision.Action, "missing user or item", checkin.OriginalPath, checkin.OriginalUserAgent)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), embyCorrectionTimeout)
	defer cancel()
	if err := p.embyMarkUnplayed(ctx, settings, r, userID, itemID); err != nil {
		playdiag.Printf("curio emby playback correction failed action=%s item=%q user=%q session=%q link=%s position_ticks=%d duration_ticks=%d err=%s path=%q request_ua=%q",
			decision.Action, itemID, userID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), decision.PositionTicks, decision.RunTimeTicks, err.Error(), checkin.OriginalPath, checkin.OriginalUserAgent)
		return
	}
	if decision.Action == playbackCorrectionSaveResume {
		if err := p.embySaveResume(ctx, settings, r, userID, itemID, decision.PositionTicks); err != nil {
			playdiag.Printf("curio emby playback correction failed action=%s item=%q user=%q session=%q link=%s position_ticks=%d duration_ticks=%d err=%s path=%q request_ua=%q",
				decision.Action, itemID, userID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), decision.PositionTicks, decision.RunTimeTicks, err.Error(), checkin.OriginalPath, checkin.OriginalUserAgent)
			return
		}
	}
	playdiag.Printf("curio emby playback correction ok action=%s item=%q user=%q session=%q link=%s position_ticks=%d duration_ticks=%d reason=%q path=%q request_ua=%q",
		decision.Action, itemID, userID, checkin.PlaySessionID, shortProxyLogValue(checkin.LinkID, 16), decision.PositionTicks, decision.RunTimeTicks, decision.Reason, checkin.OriginalPath, checkin.OriginalUserAgent)
}

func playbackCorrection(checkin playbackCheckin, state playbackSessionState) playbackCorrectionDecision {
	position := int64(0)
	if checkin.HasPositionTicks && checkin.PositionTicks > 0 {
		position = checkin.PositionTicks
	}
	if state.LastPositionTicks > position {
		position = state.LastPositionTicks
	}
	if state.MaxPositionTicks > position {
		position = state.MaxPositionTicks
	}
	duration := firstPositiveI64(state.RunTimeTicks, checkin.RunTimeTicks)
	if position <= 0 {
		return playbackCorrectionDecision{Action: playbackCorrectionClearWatched, PositionTicks: position, RunTimeTicks: duration, Reason: "invalid or empty stop position"}
	}
	if duration > 0 && position*100 >= duration*90 {
		return playbackCorrectionDecision{Action: playbackCorrectionNone, PositionTicks: position, RunTimeTicks: duration, Reason: "position reached watched threshold"}
	}
	if position < playbackShortStopTicks || (duration > 0 && position*100 < duration*5) {
		return playbackCorrectionDecision{Action: playbackCorrectionClearWatched, PositionTicks: position, RunTimeTicks: duration, Reason: "position below resume threshold"}
	}
	if position >= playbackResumeTicks {
		return playbackCorrectionDecision{Action: playbackCorrectionSaveResume, PositionTicks: position, RunTimeTicks: duration, Reason: "position below watched threshold"}
	}
	return playbackCorrectionDecision{Action: playbackCorrectionClearWatched, PositionTicks: position, RunTimeTicks: duration, Reason: "position too short to resume"}
}

func (p *Proxy) embyMarkUnplayed(ctx context.Context, settings models.P115Settings, r *http.Request, userID, itemID string) error {
	apiPath := "/Users/" + url.PathEscape(userID) + "/PlayedItems/" + url.PathEscape(itemID)
	if _, status, err := p.doEmbyRequest(ctx, settings, r, http.MethodDelete, apiPath, nil); err == nil || status == http.StatusNotFound {
		return nil
	}
	_, status, err := p.doEmbyRequest(ctx, settings, r, http.MethodPost, apiPath+"/Delete", nil)
	if err == nil || status == http.StatusNotFound {
		return nil
	}
	return err
}

func (p *Proxy) embySaveResume(ctx context.Context, settings models.P115Settings, r *http.Request, userID, itemID string, positionTicks int64) error {
	apiPath := "/Users/" + url.PathEscape(userID) + "/Items/" + url.PathEscape(itemID) + "/UserData"
	payload := map[string]any{}
	if body, _, err := p.doEmbyRequest(ctx, settings, r, http.MethodGet, apiPath, nil); err == nil && len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	payload["PlaybackPositionTicks"] = positionTicks
	payload["Played"] = false
	_, _, err := p.doEmbyRequest(ctx, settings, r, http.MethodPost, apiPath, payload)
	return err
}

func (p *Proxy) resolveEmbyUserIDFromSessions(ctx context.Context, settings models.P115Settings, r *http.Request, checkin playbackCheckin) string {
	if checkin.PlaySessionID == "" && checkin.ItemID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, _, err := p.doEmbyRequest(ctx, settings, r, http.MethodGet, "/Sessions", nil)
	if err != nil {
		return ""
	}
	var sessions []map[string]any
	if err := json.Unmarshal(body, &sessions); err != nil {
		return ""
	}
	for _, session := range sessions {
		userID := stringFromAny(session["UserId"])
		if userID == "" {
			continue
		}
		playState, _ := session["PlayState"].(map[string]any)
		if checkin.PlaySessionID != "" && firstNonEmpty(stringFromAny(session["PlaySessionId"]), stringFromAny(playState["PlaySessionId"])) == checkin.PlaySessionID {
			return userID
		}
		item, _ := session["NowPlayingItem"].(map[string]any)
		if checkin.ItemID != "" && stringFromAny(item["Id"]) == checkin.ItemID {
			return userID
		}
	}
	return ""
}

func (p *Proxy) doEmbyRequest(ctx context.Context, settings models.P115Settings, r *http.Request, method, apiPath string, payload any) ([]byte, int, error) {
	endpoint, err := embyAPIURL(settings, apiPath)
	if err != nil {
		return nil, 0, err
	}
	token := embyCorrectionToken(settings, r)
	if token == "" {
		return nil, 0, errors.New("missing Emby token")
	}
	query := endpoint.Query()
	query.Set("api_key", token)
	endpoint.RawQuery = query.Encode()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Emby-Token", token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBody, resp.StatusCode, errors.New("Emby returned " + strconv.Itoa(resp.StatusCode) + ": " + strings.TrimSpace(string(respBody)))
	}
	return respBody, resp.StatusCode, nil
}

func embyAPIURL(settings models.P115Settings, apiPath string) (*url.URL, error) {
	upstream, err := url.Parse(strings.TrimSpace(settings.EmbyUpstreamURL))
	if err != nil {
		return nil, err
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		return nil, errors.New("invalid Emby upstream URL")
	}
	next := *upstream
	next.Path = joinURLPath(upstream.Path, apiPath)
	next.RawQuery = ""
	return &next, nil
}

func embyCorrectionToken(settings models.P115Settings, r *http.Request) string {
	if token := strings.TrimSpace(settings.EmbyAPIKey); token != "" {
		return token
	}
	if r != nil && r.URL != nil {
		query := r.URL.Query()
		if token := firstNonEmpty(query.Get("api_key"), query.Get("X-Emby-Token"), query.Get("X-MediaBrowser-Token")); token != "" {
			return token
		}
	}
	if r != nil {
		if token := firstNonEmpty(r.Header.Get("X-Emby-Token"), r.Header.Get("X-MediaBrowser-Token")); token != "" {
			return token
		}
		if token := embyAuthorizationField(r.Header.Get("X-Emby-Authorization"), "Token"); token != "" {
			return token
		}
		if token := embyAuthorizationField(r.Header.Get("Authorization"), "Token"); token != "" {
			return token
		}
	}
	return ""
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
				if link, ok := p.rewriteMediaSource(ctx, r, itemID, mediaSource, rewritePath, prewarm); ok {
					applyRunTimeTicks(typed, link.MediaDurationTicks)
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
	streams, durationTicks := p.mediaStreamsForLink(ctx, r, link, prewarm, resolveUA)
	if durationTicks > 0 {
		link.MediaDurationTicks = durationTicks
	}
	applyDirectPlayMediaSource(mediaSource, r, playURL, link, rewritePath, streams)
	playdiag.Printf("curio emby rewrite source item=%q media_source=%q link=%s path=%q request_ua=%q rewrite_path=%t direct_stream=%q required_ua=%q container=%q size=%d duration_ticks=%d",
		itemID, stringFromAny(mediaSource["Id"]), shortProxyLogValue(link.ID, 16), r.URL.RequestURI(), r.UserAgent(), rewritePath, playURL, resolveUA, container, link.Size, link.MediaDurationTicks)
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

func applyDirectPlayMediaSource(mediaSource map[string]any, r *http.Request, playURL string, link models.STRMLink, rewritePath bool, streams []any) {
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
	applyRunTimeTicks(mediaSource, link.MediaDurationTicks)
	removeRequiredHeader(mediaSource, "User-Agent")
	ensureMediaStreams(mediaSource, link, streams)
	mediaSource["SupportsDirectPlay"] = true
	mediaSource["SupportsDirectStream"] = true
	mediaSource["SupportsTranscoding"] = false
	mediaSource["AddApiKeyToDirectStreamUrl"] = false
	mediaSource["SupportsProbing"] = !mediaSourceHasStreams(mediaSource)
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

func applyRunTimeTicks(value map[string]any, durationTicks int64) {
	if durationTicks <= 0 {
		return
	}
	value["RunTimeTicks"] = durationTicks
}

func mediaSourceHasStreams(mediaSource map[string]any) bool {
	streams, ok := mediaSource["MediaStreams"].([]any)
	return ok && len(streams) > 0
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

func ensureMediaStreams(mediaSource map[string]any, _ models.STRMLink, streams []any) {
	if streams, ok := mediaSource["MediaStreams"].([]any); ok && len(streams) > 0 {
		return
	}
	if len(streams) == 0 {
		if _, ok := mediaSource["DefaultSubtitleStreamIndex"]; !ok {
			mediaSource["DefaultSubtitleStreamIndex"] = -1
		}
		return
	}
	mediaSource["MediaStreams"] = streams
	if bitrate := totalStreamBitrate(streams); bitrate > 0 {
		mediaSource["Bitrate"] = bitrate
	}
	if audioIndex, ok := defaultStreamIndex(streams, "Audio"); ok {
		mediaSource["DefaultAudioStreamIndex"] = audioIndex
	}
	if subtitleIndex, ok := defaultStreamIndex(streams, "Subtitle"); ok {
		mediaSource["DefaultSubtitleStreamIndex"] = subtitleIndex
	} else if _, ok := mediaSource["DefaultSubtitleStreamIndex"]; !ok {
		mediaSource["DefaultSubtitleStreamIndex"] = -1
	}
}

func (p *Proxy) mediaStreamsForLink(ctx context.Context, r *http.Request, link models.STRMLink, allowProbe bool, resolveUA string) ([]any, int64) {
	cachedStreams, hasCachedStreams := decodeStoredMediaStreams(link.MediaStreams)
	if hasCachedStreams && link.MediaDurationTicks > 0 {
		return cachedStreams, link.MediaDurationTicks
	}
	if strings.TrimSpace(link.MediaProbeError) != "" && link.MediaProbedAt != nil && time.Since(*link.MediaProbedAt) < 6*time.Hour {
		if hasCachedStreams {
			return cachedStreams, link.MediaDurationTicks
		}
		return nil, 0
	}
	if !allowProbe || p == nil || p.play == nil || p.store == nil {
		if hasCachedStreams {
			return cachedStreams, link.MediaDurationTicks
		}
		return nil, 0
	}
	streams, durationTicks, err := p.probeMediaStreamsForLink(ctx, r, link, resolveUA)
	if err != nil {
		_ = p.store.UpdateSTRMLinkMediaStreams(ctx, link.ID, "", 0, err.Error())
		playdiag.Printf("curio emby media probe failed link=%s path=%q err=%s", shortProxyLogValue(link.ID, 16), link.RelativePath, err.Error())
		if hasCachedStreams {
			return cachedStreams, link.MediaDurationTicks
		}
		return nil, 0
	}
	body, err := json.Marshal(streams)
	if err != nil {
		playdiag.Printf("curio emby media probe marshal failed link=%s path=%q err=%s", shortProxyLogValue(link.ID, 16), link.RelativePath, err.Error())
		return streams, durationTicks
	}
	_ = p.store.UpdateSTRMLinkMediaStreams(ctx, link.ID, string(body), durationTicks, "")
	playdiag.Printf("curio emby media probe ok link=%s path=%q streams=%d duration_ticks=%d", shortProxyLogValue(link.ID, 16), link.RelativePath, len(streams), durationTicks)
	return streams, durationTicks
}

func (p *Proxy) probeMediaStreamsForLink(ctx context.Context, r *http.Request, link models.STRMLink, resolveUA string) ([]any, int64, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	directURL, err := p.play.ResolvePlayURLFromRoute(probeCtx, "id/"+link.ID, publicBase(r), resolveUA)
	if err != nil {
		return nil, 0, err
	}
	source := mediainfo.Source{
		URL:       mediainfo.CleanURL(directURL),
		UserAgent: resolveUA,
		Extension: strings.TrimPrefix(strings.ToLower(path.Ext(firstNonEmpty(link.RelativePath, link.RemotePath, link.STRMPath))), "."),
		Size:      link.Size,
	}
	detailed, err := mediainfo.ProbeDetailed(probeCtx, source)
	if err != nil {
		return nil, 0, err
	}
	streams := embyMediaStreamsFromProbe(detailed.Streams, link)
	if len(streams) == 0 {
		return nil, 0, errors.New("ffprobe returned no usable media streams")
	}
	return streams, detailed.DurationTicks, nil
}

func decodeStoredMediaStreams(value string) ([]any, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	var streams []any
	if err := json.Unmarshal([]byte(value), &streams); err != nil || len(streams) == 0 {
		return nil, false
	}
	return streams, true
}

func embyMediaStreamsFromProbe(streams []mediainfo.Stream, link models.STRMLink) []any {
	out := make([]any, 0, len(streams))
	for fallbackIndex, stream := range streams {
		index := stream.Index
		if index < 0 {
			index = fallbackIndex
		}
		switch stream.Type {
		case "video":
			out = append(out, embyVideoStream(index, stream, link))
		case "audio":
			out = append(out, embyAudioStream(index, stream))
		case "subtitle":
			out = append(out, embySubtitleStream(index, stream))
		}
	}
	return out
}

func embyVideoStream(index int, stream mediainfo.Stream, link models.STRMLink) map[string]any {
	width := firstPositive(stream.Width, stream.CodedWidth)
	height := firstPositive(stream.Height, stream.CodedHeight)
	item := baseEmbyStream(index, "Video", stream)
	item["Codec"] = embyCodec(stream.Codec)
	if stream.CodecTag != "" {
		item["CodecTag"] = stream.CodecTag
	}
	if width > 0 {
		item["Width"] = width
	}
	if height > 0 {
		item["Height"] = height
	}
	if width > 0 && height > 0 {
		item["AspectRatio"] = aspectRatio(width, height)
	}
	if value := frameRateValue(stream.AverageFrameRate); value > 0 {
		item["AverageFrameRate"] = value
	}
	if value := frameRateValue(stream.RealFrameRate); value > 0 {
		item["RealFrameRate"] = value
	}
	if stream.BitRate > 0 {
		item["BitRate"] = stream.BitRate
	}
	item["DisplayTitle"] = videoDisplayTitle(stream, link)
	item["IsDefault"] = true
	item["IsInterlaced"] = false
	item["IsTextSubtitleStream"] = false
	item["VideoRange"] = firstNonEmpty(stream.VideoRange, "SDR")
	return item
}

func embyAudioStream(index int, stream mediainfo.Stream) map[string]any {
	item := baseEmbyStream(index, "Audio", stream)
	item["Codec"] = embyCodec(stream.Codec)
	if stream.CodecTag != "" {
		item["CodecTag"] = stream.CodecTag
	}
	if stream.Profile != "" {
		item["Profile"] = stream.Profile
	}
	if stream.Channels > 0 {
		item["Channels"] = stream.Channels
	}
	if stream.ChannelLayout != "" {
		item["ChannelLayout"] = stream.ChannelLayout
	}
	if stream.SampleRate > 0 {
		item["SampleRate"] = stream.SampleRate
	}
	if stream.BitRate > 0 {
		item["BitRate"] = stream.BitRate
	}
	item["DisplayTitle"] = audioDisplayTitle(stream)
	item["IsDefault"] = stream.Default
	item["IsTextSubtitleStream"] = false
	return item
}

func embySubtitleStream(index int, stream mediainfo.Stream) map[string]any {
	item := baseEmbyStream(index, "Subtitle", stream)
	item["Codec"] = embyCodec(stream.Codec)
	item["DisplayTitle"] = subtitleDisplayTitle(stream)
	item["IsDefault"] = stream.Default
	item["IsTextSubtitleStream"] = stream.TextSubtitle
	return item
}

func baseEmbyStream(index int, streamType string, stream mediainfo.Stream) map[string]any {
	return map[string]any{
		"Index":                  index,
		"Type":                   streamType,
		"Protocol":               "Http",
		"Language":               firstNonEmpty(stream.Language, "und"),
		"IsDefault":              stream.Default,
		"IsExternal":             false,
		"IsForced":               stream.Forced,
		"IsHearingImpaired":      stream.HearingImpaired,
		"IsInterlaced":           false,
		"SupportsExternalStream": false,
	}
}

func defaultStreamIndex(streams []any, streamType string) (int, bool) {
	first := 0
	hasFirst := false
	for _, raw := range streams {
		stream, ok := raw.(map[string]any)
		if !ok || stringFromAny(stream["Type"]) != streamType {
			continue
		}
		index, ok := intFromAny(stream["Index"])
		if !ok {
			continue
		}
		if !hasFirst {
			first = index
			hasFirst = true
		}
		if value, ok := stream["IsDefault"].(bool); ok && value {
			return index, true
		}
	}
	return first, hasFirst
}

func totalStreamBitrate(streams []any) int64 {
	var total int64
	for _, raw := range streams {
		stream, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch value := stream["BitRate"].(type) {
		case int64:
			total += value
		case int:
			total += int64(value)
		case float64:
			total += int64(value)
		}
	}
	return total
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func videoDisplayTitle(stream mediainfo.Stream, link models.STRMLink) string {
	parts := []string{}
	if container := strings.ToUpper(mediaContainerForLink(link)); container != "" {
		parts = append(parts, container)
	}
	if res := resolutionTitle(stream); res != "" {
		parts = append(parts, res)
	}
	if codec := displayCodec(stream); codec != "" {
		parts = append(parts, codec)
	}
	if rangeText := firstNonEmpty(stream.VideoRange, ""); rangeText != "" && rangeText != "SDR" {
		parts = append(parts, rangeText)
	}
	return strings.Join(parts, " ")
}

func audioDisplayTitle(stream mediainfo.Stream) string {
	parts := []string{}
	if codec := displayCodec(stream); codec != "" {
		parts = append(parts, codec)
	}
	if channels := audioChannelsTitle(stream); channels != "" {
		parts = append(parts, channels)
	}
	if language := firstNonEmpty(stream.Language, ""); language != "" && language != "und" {
		parts = append(parts, language)
	}
	if stream.Title != "" {
		parts = append(parts, stream.Title)
	}
	if len(parts) == 0 {
		return "Audio"
	}
	return strings.Join(parts, " ")
}

func subtitleDisplayTitle(stream mediainfo.Stream) string {
	parts := []string{}
	if language := firstNonEmpty(stream.Language, ""); language != "" && language != "und" {
		parts = append(parts, language)
	}
	if codec := displayCodec(stream); codec != "" {
		parts = append(parts, codec)
	}
	if stream.Forced {
		parts = append(parts, "Forced")
	}
	if stream.Title != "" {
		parts = append(parts, stream.Title)
	}
	if len(parts) == 0 {
		return "Subtitle"
	}
	return strings.Join(parts, " ")
}

func displayCodec(stream mediainfo.Stream) string {
	switch strings.ToLower(strings.TrimSpace(stream.Codec)) {
	case "h264", "avc1", "avc":
		return "AVC"
	case "hevc", "h265", "hev1", "hvc1":
		return "HEVC"
	case "mpeg2video":
		return "MPEG-2"
	case "vc1":
		return "VC-1"
	case "truehd":
		return "TrueHD"
	case "eac3":
		return "DDP"
	case "ac3":
		return "AC3"
	case "dts":
		return firstNonEmpty(stream.Profile, "DTS")
	case "pgssub":
		return "PGS"
	case "":
		return ""
	default:
		return strings.ToUpper(stream.Codec)
	}
}

func embyCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "pgssub":
		return "pgs"
	case "subrip":
		return "srt"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}

func resolutionTitle(stream mediainfo.Stream) string {
	height := firstPositive(stream.Height, stream.CodedHeight)
	width := firstPositive(stream.Width, stream.CodedWidth)
	switch {
	case height >= 4300 || width >= 7600:
		return "4320p"
	case height >= 2000 || width >= 3800:
		return "2160p"
	case height >= 1000 || width >= 1900:
		return "1080p"
	case height >= 700 || width >= 1200:
		return "720p"
	case height > 0:
		return strconv.Itoa(height) + "p"
	default:
		return ""
	}
}

func audioChannelsTitle(stream mediainfo.Stream) string {
	layout := strings.ToLower(stream.ChannelLayout)
	for _, token := range []string{"7.1.4", "7.1", "6.1", "5.1.4", "5.1.2", "5.1", "2.0", "1.0"} {
		if strings.Contains(layout, token) {
			return token
		}
	}
	switch stream.Channels {
	case 8:
		return "7.1"
	case 7:
		return "6.1"
	case 6:
		return "5.1"
	case 2:
		return "2.0"
	case 1:
		return "1.0"
	default:
		if stream.Channels > 0 {
			return strconv.Itoa(stream.Channels)
		}
		return ""
	}
}

func frameRateValue(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if strings.Contains(value, "/") {
		parts := strings.SplitN(value, "/", 2)
		num, _ := strconv.ParseFloat(parts[0], 64)
		den, _ := strconv.ParseFloat(parts[1], 64)
		if den == 0 {
			return 0
		}
		return num / den
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}

func aspectRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	divisor := gcd(width, height)
	return strconv.Itoa(width/divisor) + ":" + strconv.Itoa(height/divisor)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a <= 0 {
		return 1
	}
	return a
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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

func int64FromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed, true
		}
		floatValue, err := strconv.ParseFloat(v.String(), 64)
		if err != nil {
			return 0, false
		}
		return int64(floatValue), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case string:
		return int64FromString(v)
	default:
		return 0, false
	}
}

func int64FromString(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil
}

func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		return boolFromString(v)
	default:
		return false, false
	}
}

func boolFromString(value string) (bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(value)
	return parsed, err == nil
}

func firstPositiveI64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func unescapePathValue(value string) string {
	if unescaped, err := url.PathUnescape(value); err == nil {
		return unescaped
	}
	return value
}

func embyAuthorizationField(header, key string) string {
	header = strings.TrimSpace(header)
	key = strings.ToLower(strings.TrimSpace(key))
	if header == "" || key == "" {
		return ""
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if i := strings.Index(part, " "); i >= 0 {
			part = strings.TrimSpace(part[i+1:])
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok || strings.ToLower(strings.TrimSpace(name)) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
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
