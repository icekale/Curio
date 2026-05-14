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

	"curio/internal/models"
	"curio/internal/p115"
	"curio/internal/repository"
)

type Proxy struct {
	store *repository.Store
	play  *p115.Service
}

var (
	playbackInfoPattern = regexp.MustCompile(`(?i)^/?Items/([^/]+)/PlaybackInfo/?$`)
	streamPattern       = regexp.MustCompile(`(?i)^/?(?:Videos|Audio)/([^/]+)/(?:stream|master\.m3u8|hls|main\.m3u8)`)
)

func New(store *repository.Store, play *p115.Service) *Proxy {
	return &Proxy{store: store, play: play}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	settings, err := p.store.P115Settings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(settings.EmbyUpstreamURL) == "" {
		http.Error(w, "Emby 原始地址未配置", http.StatusNotFound)
		return
	}
	if strings.HasPrefix(strings.TrimLeft(r.URL.Path, "/"), "play/115/") {
		p.servePlay115(w, r, settings)
		return
	}
	itemID := streamItemID(proxyPath(settings, r.URL.Path))
	if itemID != "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		if link, err := p.store.STRMLinkByEmbyItem(r.Context(), "default", itemID); err == nil && link.Status == models.STRMStatusGenerated {
			playURL, err := p.play.PlayURLForLinkName(link.ID, publicBase(settings, r), playRouteName(link))
			if err == nil {
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, playURL, http.StatusFound)
				return
			}
		}
	}
	p.reverseProxy(settings).ServeHTTP(w, r)
}

func (p *Proxy) servePlay115(w http.ResponseWriter, r *http.Request, settings models.P115Settings) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	route := tokenFromPlayURL(r.URL.RequestURI())
	if route == "" {
		http.Error(w, "播放地址无效", http.StatusBadRequest)
		return
	}
	directURL, err := p.play.ResolvePlayURLFromRoute(r.Context(), route, publicBase(settings, r), r.UserAgent())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, directURL, http.StatusFound)
}

func (p *Proxy) reverseProxy(settings models.P115Settings) http.Handler {
	upstream, _ := url.Parse(settings.EmbyUpstreamURL)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		incomingPath := proxyPath(settings, req.URL.Path)
		originalDirector(req)
		req.URL.Path = joinURLPath(upstream.Path, incomingPath)
		req.Host = upstream.Host
		req.Header.Del("Accept-Encoding")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if !isPlaybackInfo(proxyPath(settings, resp.Request.URL.Path)) {
			return nil
		}
		return p.rewritePlaybackInfo(resp, settings)
	}
	return proxy
}

func (p *Proxy) rewritePlaybackInfo(resp *http.Response, settings models.P115Settings) error {
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
		link, err := p.linkFromMediaSource(resp.Request.Context(), mediaSource)
		if err != nil {
			continue
		}
		playURL, err := p.play.PlayURLForLinkName(link.ID, publicBase(settings, resp.Request), playRouteName(link))
		if err != nil {
			continue
		}
		mediaSource["Path"] = playURL
		mediaSource["DirectStreamUrl"] = playURL
		mediaSource["Protocol"] = "Http"
		mediaSource["IsRemote"] = true
		mediaSource["SupportsDirectPlay"] = true
		mediaSource["SupportsDirectStream"] = true
		mediaSource["SupportsTranscoding"] = false
		if itemID != "" {
			_ = p.store.UpsertEmbySTRMItem(resp.Request.Context(), models.EmbySTRMItem{
				ID:           stableEmbyID("default", itemID),
				EmbyServerID: "default",
				EmbyItemID:   itemID,
				STRMLinkID:   link.ID,
				STRMPath:     link.STRMPath,
				Status:       "active",
			})
		}
		changed = true
	}
	if !changed {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}
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
	return models.STRMLink{}, errors.New("未命中 STRM 链接")
}

func proxyPath(settings models.P115Settings, raw string) string {
	base := strings.TrimRight(settings.EmbyProxyBasePath, "/")
	if base == "" {
		base = "/emby"
	}
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
	if len(match) == 2 {
		return match[1]
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

func publicBase(settings models.P115Settings, r *http.Request) string {
	if strings.TrimSpace(settings.PublicBaseURL) != "" {
		return strings.TrimRight(settings.PublicBaseURL, "/")
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
