package p115

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"curio/internal/models"
	"curio/internal/repository"
)

type Service struct {
	store         *repository.Store
	cacheMu       sync.Mutex
	directCache   map[string]cachedDirectURL
	authMu        sync.Mutex
	qrSessions    map[string]qrAuthSession
	oauthSessions map[string]oauthSession
}

type cachedDirectURL struct {
	URL       string
	ExpiresAt time.Time
}

type qrAuthSession struct {
	UID          string
	Kind         string
	App          string
	CodeVerifier string
	Time         string
	Sign         string
	ExpiresAt    time.Time
}

type oauthSession struct {
	State       string
	RedirectURI string
	ExpiresAt   time.Time
}

const defaultDirectURLTTL = 5 * time.Minute

func NewService(store *repository.Store) *Service {
	return &Service{
		store:         store,
		directCache:   map[string]cachedDirectURL{},
		qrSessions:    map[string]qrAuthSession{},
		oauthSessions: map[string]oauthSession{},
	}
}

func (s *Service) Status(ctx context.Context) (models.P115Status, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115Status{}, err
	}
	return NewClient(settings).Status(ctx)
}

func (s *Service) StartQRCode(ctx context.Context) (models.P115QRCodeSession, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115QRCodeSession{}, err
	}
	session, err := NewClient(settings).StartCookieQRCode(ctx)
	if err != nil {
		return models.P115QRCodeSession{}, err
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	s.authMu.Lock()
	s.qrSessions[session.UID] = qrAuthSession{
		UID:       session.UID,
		Kind:      authModeCookies,
		App:       NormalizeCookieLoginApp(settings.CookieLoginApp),
		Time:      session.Time,
		Sign:      session.Sign,
		ExpiresAt: expiresAt,
	}
	s.authMu.Unlock()
	return models.P115QRCodeSession{
		UID:       session.UID,
		QRCodeURL: session.QRCodeURL,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) QRCodeStatus(ctx context.Context, uid string) (models.P115QRCodeStatus, error) {
	session, err := s.qrSession(uid)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	status, err := NewClient(settings).OpenQRCodeStatus(ctx, session.UID, session.Time, session.Sign)
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	return status, nil
}

func (s *Service) CompleteQRCode(ctx context.Context, uid string) (models.P115AuthResult, error) {
	session, err := s.qrSession(uid)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	client := NewClient(settings)
	if session.Kind == authModeOpen {
		tokens, err := client.OpenDeviceCodeToToken(ctx, session.UID, session.CodeVerifier)
		if err != nil {
			return models.P115AuthResult{}, err
		}
		if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
			return models.P115AuthResult{}, err
		}
	} else {
		cookies, err := client.CookieQRCodeToCookies(ctx, session.UID, session.App)
		if err != nil {
			return models.P115AuthResult{}, err
		}
		if err := s.saveCookies(ctx, cookies); err != nil {
			return models.P115AuthResult{}, err
		}
	}
	s.authMu.Lock()
	delete(s.qrSessions, session.UID)
	s.authMu.Unlock()
	return models.P115AuthResult{Status: "ok", Message: "115 Cookies 已保存"}, nil
}

func (s *Service) StartOAuth(ctx context.Context, fallbackBaseURL string) (models.P115OAuthStart, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115OAuthStart{}, err
	}
	if strings.TrimSpace(settings.AppID) == "" {
		return models.P115OAuthStart{}, errors.New("请先填写 115 App ID")
	}
	if strings.TrimSpace(settings.AppSecret) == "" {
		return models.P115OAuthStart{}, errors.New("请先填写 115 App Secret")
	}
	baseURL := strings.TrimSpace(settings.PublicBaseURL)
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	redirectURI := joinPublicURL(baseURL, "/api/p115/auth/oauth/callback")
	state, err := randomToken(18)
	if err != nil {
		return models.P115OAuthStart{}, err
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	s.authMu.Lock()
	s.oauthSessions[state] = oauthSession{State: state, RedirectURI: redirectURI, ExpiresAt: expiresAt}
	s.authMu.Unlock()
	return NewClient(settings).OpenOAuthAuthorizeURL(redirectURI, state), nil
}

func (s *Service) CompleteOAuth(ctx context.Context, code, state string) (models.P115AuthResult, error) {
	session, err := s.oauthSession(state)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	tokens, err := NewClient(settings).OpenAuthCodeToToken(ctx, code, session.RedirectURI)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	s.authMu.Lock()
	delete(s.oauthSessions, state)
	s.authMu.Unlock()
	return models.P115AuthResult{Status: "ok", Message: "115 OAuth 登录成功"}, nil
}

func (s *Service) RefreshOpenToken(ctx context.Context) (models.P115AuthResult, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if strings.TrimSpace(settings.RefreshToken) == "" {
		return models.P115AuthResult{}, errors.New("当前没有可刷新的 115 Refresh Token")
	}
	tokens, err := NewClient(settings).RefreshOpenToken(ctx)
	if err != nil {
		return models.P115AuthResult{}, err
	}
	if err := s.saveOpenTokens(ctx, tokens.AccessToken, tokens.RefreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	return models.P115AuthResult{Status: "ok", Message: "115 令牌已刷新"}, nil
}

func (s *Service) ImportOpenToken(ctx context.Context, accessToken, refreshToken string) (models.P115AuthResult, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return models.P115AuthResult{}, errors.New("请填写 Refresh Token，用于后续刷新授权")
	}
	if err := s.saveOpenTokens(ctx, accessToken, refreshToken); err != nil {
		return models.P115AuthResult{}, err
	}
	return models.P115AuthResult{Status: "ok", Message: "OpenList Token 已导入"}, nil
}

func (s *Service) ExportTree(ctx context.Context) (models.STRMSyncResult, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion()}
	for _, lib := range cfg.Libraries {
		items, err := client.ExportTree(ctx, lib)
		if err != nil {
			result.Failed++
			return result, err
		}
		result.Exported += len(items)
		for _, item := range items {
			if isMediaTreeItem(item) {
				result.Skipped++
			}
		}
	}
	return result, nil
}

func (s *Service) Sync(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion()}
	for _, lib := range cfg.Libraries {
		items, err := client.ExportTree(ctx, lib)
		if err != nil {
			result.Failed++
			return result, err
		}
		result.Exported += len(items)
		snapshot := make([]models.P115TreeSnapshotItem, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			item.SourceTreeHash = sourceHash(lib.CID, item.RelativePath, item.RemoteFileID, item.PickCode, item.SHA1, item.Size)
			media := isMediaTreeItem(item)
			snapshot = append(snapshot, models.P115TreeSnapshotItem{
				LibraryCID:     lib.CID,
				TreeVersion:    result.TreeVersion,
				RelativePath:   item.RelativePath,
				Name:           item.Name,
				Extension:      strings.TrimPrefix(strings.ToLower(path.Ext(item.Name)), "."),
				Depth:          item.Depth,
				IsMedia:        media,
				SourceTreeHash: item.SourceTreeHash,
			})
			if !media {
				continue
			}
			seen[item.RelativePath] = struct{}{}
		}
		existing, err := s.store.ActiveSTRMLinksByLibrary(ctx, models.STRMProvider115, lib.CID)
		if err != nil {
			return result, err
		}
		existingByPath := make(map[string]models.STRMLink, len(existing))
		for _, link := range existing {
			existingByPath[link.RelativePath] = link
		}
		for _, item := range items {
			if !isMediaTreeItem(item) {
				continue
			}
			link, err := s.linkForItem(settings, fallbackBaseURL, lib, item, result.TreeVersion)
			if err != nil {
				result.Failed++
				continue
			}
			old, existed := existingByPath[item.RelativePath]
			if existed && old.PickCode != "" && link.PickCode == "" {
				link.PickCode = old.PickCode
				link.RemoteFileID = old.RemoteFileID
				link.SHA1 = old.SHA1
				link.Size = old.Size
				link.ResolveStatus = old.ResolveStatus
				link.ResolvedAt = old.ResolvedAt
			}
			localExists := fileExists(link.STRMPath)
			if err := writeSTRM(settings.STRMOutputPath, link.STRMPath, link.PlayPath); err != nil {
				result.Failed++
				_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusFailed, models.STRMResolveFailed, "STRM_WRITE_FAILED", err.Error())
				continue
			}
			if err := s.store.UpsertSTRMLink(ctx, link); err != nil {
				result.Failed++
				continue
			}
			if !existed {
				result.Generated++
			} else if !localExists {
				result.Restored++
			} else if old.SourceTreeHash != link.SourceTreeHash || old.STRMPath != link.STRMPath || old.PlayPath != link.PlayPath {
				result.Updated++
			} else {
				result.Skipped++
			}
		}
		for _, link := range existing {
			if _, ok := seen[link.RelativePath]; ok {
				continue
			}
			if err := s.markMissing(ctx, settings, link); err != nil {
				result.Failed++
				continue
			}
			result.Deleted++
		}
		if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, result.TreeVersion, snapshot); err != nil {
			return result, err
		}
	}
	if settings.RefreshEmbyAfterSync {
		_ = refreshEmby(ctx, settings)
	}
	return result, nil
}

func (s *Service) Cleanup(ctx context.Context) (models.STRMSyncResult, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	links, err := s.store.STRMLinksByStatuses(ctx, []string{models.STRMStatusStale, models.STRMStatusDeleted, models.STRMStatusFailed})
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	result := models.STRMSyncResult{TreeVersion: treeVersion()}
	for _, link := range links {
		if removeManagedSTRM(settings.STRMOutputPath, link.STRMPath) == nil {
			result.Deleted++
		} else {
			result.Failed++
		}
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusDeleted, models.STRMResolveStale, "", "")
	}
	return result, nil
}

func (s *Service) PlayURLForLink(linkID, baseURL string) (string, error) {
	token, err := signPlayToken(linkID, 10*365*24*time.Hour)
	if err != nil {
		return "", err
	}
	return joinPublicURL(baseURL, "/play/115/"+token), nil
}

func (s *Service) PlayURLForLinkName(linkID, baseURL, displayName string) (string, error) {
	name := cleanPlayDisplayName(displayName)
	if name == "" {
		name = path.Join("id", strings.TrimSpace(linkID))
	}
	return joinPublicURLReadable(baseURL, "/play/115/"+name), nil
}

func (s *Service) LinkIDFromToken(token string) (string, error) {
	return verifyPlayToken(token)
}

func (s *Service) ResolvePlayURL(ctx context.Context, token, requestUserAgent string) (string, error) {
	linkID, err := verifyPlayToken(token)
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
}

func (s *Service) ResolvePlayURLFromRoute(ctx context.Context, route, baseURL, requestUserAgent string) (string, error) {
	route = cleanPlayDisplayName(route)
	if route == "" {
		return "", errors.New("播放地址无效")
	}
	if linkID, err := verifyPlayToken(route); err == nil {
		return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
	}
	if strings.HasPrefix(route, "id/") {
		linkID := strings.TrimSpace(strings.TrimPrefix(route, "id/"))
		if linkID != "" {
			return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
		}
	}
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLinkByPlayRoute(ctx, models.STRMProvider115, route, playPathCandidates(baseURL, route))
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, cfg, link, requestUserAgent)
}

func (s *Service) resolvePlayURLByLinkID(ctx context.Context, linkID, requestUserAgent string) (string, error) {
	settings, cfg, err := s.settingsAndLibraries(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLink(ctx, linkID)
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, cfg, link, requestUserAgent)
}

func (s *Service) resolvePlayURLForLink(ctx context.Context, settings models.P115Settings, cfg LibrariesConfig, link models.STRMLink, requestUserAgent string) (string, error) {
	if link.Provider != models.STRMProvider115 || link.Status != models.STRMStatusGenerated {
		return "", errors.New("STRM 链接不可播放")
	}
	if _, ok := libraryCIDs(cfg)[link.LibraryCID]; !ok {
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusStale, models.STRMResolveStale, "CID_NOT_CONFIGURED", "115 媒体库 CID 已不在当前配置中")
		return "", errors.New("115 媒体库 CID 已不在当前配置中")
	}
	client := NewClient(settings)
	if strings.TrimSpace(link.PickCode) == "" {
		info, err := client.ResolvePath(ctx, link.LibraryCID, link.RelativePath)
		if err != nil {
			_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, link.Status, models.STRMResolveFailed, "RESOLVE_PATH_FAILED", err.Error())
			return "", err
		}
		link.RemoteFileID = info.ID
		link.PickCode = info.PickCode
		link.SHA1 = info.SHA1
		link.Size = info.Size
		if err := s.store.UpdateSTRMLinkResolved(ctx, link.ID, info.ID, info.PickCode, info.SHA1, info.Size); err != nil {
			return "", err
		}
	}
	ua := userAgent(settings, requestUserAgent)
	if directURL, ok := s.cachedDirectURL(link.PickCode, ua); ok {
		return directURL, nil
	}
	directURL, err := client.DirectURL(ctx, link.PickCode, ua)
	if err != nil {
		_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, link.Status, models.STRMResolveFailed, "DIRECT_URL_FAILED", err.Error())
		return "", err
	}
	s.rememberDirectURL(link.PickCode, ua, directURL, defaultDirectURLTTL)
	return directURL, nil
}

func (s *Service) cachedDirectURL(pickcode, userAgentValue string) (string, bool) {
	key := directCacheKey(pickcode, userAgentValue)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	item, ok := s.directCache[key]
	if !ok || time.Now().After(item.ExpiresAt) {
		delete(s.directCache, key)
		return "", false
	}
	return item.URL, true
}

func (s *Service) rememberDirectURL(pickcode, userAgentValue, directURL string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	key := directCacheKey(pickcode, userAgentValue)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.directCache[key] = cachedDirectURL{URL: directURL, ExpiresAt: time.Now().Add(ttl)}
}

func (s *Service) qrSession(uid string) (qrAuthSession, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return qrAuthSession{}, errors.New("扫码会话为空")
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	session, ok := s.qrSessions[uid]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.qrSessions, uid)
		return qrAuthSession{}, errors.New("扫码会话已过期，请重新生成二维码")
	}
	return session, nil
}

func (s *Service) oauthSession(state string) (oauthSession, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return oauthSession{}, errors.New("OAuth state 为空")
	}
	s.authMu.Lock()
	defer s.authMu.Unlock()
	session, ok := s.oauthSessions[state]
	if !ok || time.Now().After(session.ExpiresAt) {
		delete(s.oauthSessions, state)
		return oauthSession{}, errors.New("OAuth 会话已过期，请重新发起登录")
	}
	return session, nil
}

func (s *Service) saveOpenTokens(ctx context.Context, accessToken, refreshToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	refreshToken = strings.TrimSpace(refreshToken)
	if accessToken == "" {
		return errors.New("115 未返回 Access Token")
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return err
	}
	settings.Enabled = true
	settings.AuthMode = authModeOpen
	settings.AccessToken = accessToken
	if refreshToken != "" {
		settings.RefreshToken = refreshToken
	}
	settings.DirectURLTTLSeconds = int(defaultDirectURLTTL / time.Second)
	settings.UserAgentMode = "inherit"
	settings.FixedUserAgent = ""
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	_, err = s.store.SaveP115Settings(ctx, settings)
	return err
}

func (s *Service) saveCookies(ctx context.Context, cookies string) error {
	cookies = strings.TrimSpace(cookies)
	if cookies == "" {
		return errors.New("115 未返回 Cookies")
	}
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return err
	}
	settings.Enabled = true
	settings.Cookies = cookies
	settings.AuthMode = authModeCookies
	settings.CookieLoginApp = NormalizeCookieLoginApp(settings.CookieLoginApp)
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	_, err = s.store.SaveP115Settings(ctx, settings)
	return err
}

func (s *Service) settingsAndLibraries(ctx context.Context) (models.P115Settings, LibrariesConfig, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115Settings{}, LibrariesConfig{}, err
	}
	if !settings.Enabled {
		return models.P115Settings{}, LibrariesConfig{}, errors.New("115 播放未启用")
	}
	cfg, err := ParseLibraries(settings.LibrariesYAML)
	if err != nil {
		return models.P115Settings{}, LibrariesConfig{}, err
	}
	if strings.TrimSpace(settings.STRMOutputPath) == "" {
		return models.P115Settings{}, LibrariesConfig{}, errors.New("STRM 输出目录不能为空")
	}
	return settings, cfg, nil
}

func (s *Service) linkForItem(settings models.P115Settings, fallbackBaseURL string, lib LibraryConfig, item TreeItem, version string) (models.STRMLink, error) {
	strmPath, err := strmPathFor(settings.STRMOutputPath, lib.OutputPrefix, item.RelativePath)
	if err != nil {
		return models.STRMLink{}, err
	}
	id := stableLinkID(lib.CID, item.RelativePath)
	baseURL := playBaseURL(settings, fallbackBaseURL)
	displayName := item.RelativePath
	if strings.TrimSpace(displayName) == "" {
		displayName = item.Name
	}
	playPath, err := s.PlayURLForLinkName(id, baseURL, displayName)
	if err != nil {
		return models.STRMLink{}, err
	}
	resolveStatus := models.STRMResolvePending
	var resolvedAt *time.Time
	if strings.TrimSpace(item.PickCode) != "" {
		now := time.Now()
		resolveStatus = models.STRMResolveResolved
		resolvedAt = &now
	}
	now := time.Now()
	return models.STRMLink{
		ID:             id,
		Provider:       models.STRMProvider115,
		LibraryCID:     lib.CID,
		LibraryName:    lib.Name,
		LibraryType:    lib.Type,
		RelativePath:   item.RelativePath,
		RemotePath:     "/" + path.Join(lib.Name, item.RelativePath),
		RemoteFileID:   item.RemoteFileID,
		PickCode:       item.PickCode,
		SHA1:           item.SHA1,
		Size:           item.Size,
		STRMPath:       strmPath,
		PlayPath:       playPath,
		SourceTreeHash: item.SourceTreeHash,
		TreeVersion:    version,
		ResolveStatus:  resolveStatus,
		Status:         models.STRMStatusGenerated,
		GeneratedAt:    now,
		ResolvedAt:     resolvedAt,
		UpdatedAt:      now,
	}, nil
}

func playBaseURL(settings models.P115Settings, fallbackBaseURL string) string {
	if value := strings.TrimSpace(settings.PublicBaseURL); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.EmbyPublicURL); value != "" {
		return value
	}
	return fallbackBaseURL
}

func (s *Service) markMissing(ctx context.Context, settings models.P115Settings, link models.STRMLink) error {
	if settings.DeleteMissingSTRM && !(settings.StaleBeforeDelete && link.Status == models.STRMStatusGenerated) {
		if err := removeManagedSTRM(settings.STRMOutputPath, link.STRMPath); err != nil {
			return err
		}
		return s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusDeleted, models.STRMResolveStale, "", "")
	}
	return s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusStale, models.STRMResolveStale, "", "")
}

func isMediaTreeItem(item TreeItem) bool {
	if item.IsDirectory {
		return false
	}
	return mediaExtension(path.Ext(item.Name))
}

func stableLinkID(libraryCID, relativePath string) string {
	sum := sha256.Sum256([]byte(models.STRMProvider115 + ":" + libraryCID + ":" + strings.ToLower(relativePath)))
	return hex.EncodeToString(sum[:])
}

func sourceHash(libraryCID, relativePath, fileID, pickcode, sha1 string, size int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%s:%d", libraryCID, relativePath, fileID, pickcode, sha1, size)))
	return hex.EncodeToString(sum[:])
}

func directCacheKey(pickcode, userAgentValue string) string {
	sum := sha256.Sum256([]byte(userAgentValue))
	return pickcode + ":" + hex.EncodeToString(sum[:])
}

func refreshEmby(ctx context.Context, settings models.P115Settings) error {
	upstream := strings.TrimRight(strings.TrimSpace(settings.EmbyUpstreamURL), "/")
	if upstream == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream+"/Library/Refresh", nil)
	if err != nil {
		return err
	}
	if key := strings.TrimSpace(settings.EmbyAPIKey); key != "" {
		req.Header.Set("X-Emby-Token", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Emby 刷新失败：HTTP %d", resp.StatusCode)
	}
	return nil
}

func treeVersion() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func strmPathFor(root, outputPrefix, relativePath string) (string, error) {
	relativePath = strings.Trim(strings.ReplaceAll(relativePath, "\\", "/"), "/")
	if relativePath == "" || strings.Contains(relativePath, "../") || strings.HasPrefix(relativePath, "..") {
		return "", errors.New("115 STRM 相对路径无效")
	}
	ext := path.Ext(relativePath)
	strmRel := strings.TrimSuffix(relativePath, ext) + ".strm"
	parts := []string{root}
	for _, part := range splitRelativePath(outputPrefix) {
		parts = append(parts, part)
	}
	for _, part := range splitRelativePath(strmRel) {
		parts = append(parts, part)
	}
	target := filepath.Join(parts...)
	if !insideRoot(root, target) {
		return "", errors.New("STRM 路径越界")
	}
	return target, nil
}

func writeSTRM(root, target, playPath string) error {
	if strings.TrimSpace(playPath) == "" {
		return errors.New("STRM 播放地址为空")
	}
	if !insideRoot(root, target) {
		return errors.New("STRM 路径越界")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(playPath+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func fileExists(target string) bool {
	info, err := os.Stat(target)
	return err == nil && !info.IsDir()
}

func removeManagedSTRM(root, target string) error {
	if strings.TrimSpace(target) == "" {
		return nil
	}
	if !insideRoot(root, target) {
		return errors.New("拒绝删除 STRM 输出目录外的文件")
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	removeEmptyParents(filepath.Dir(target), root)
	return nil
}

func removeEmptyParents(start, root string) {
	current, err := filepath.Abs(start)
	if err != nil {
		return
	}
	stop, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for current != stop && insideRoot(stop, current) {
		if err := os.Remove(current); err != nil {
			return
		}
		next := filepath.Dir(current)
		if next == current {
			return
		}
		current = next
	}
}

func insideRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func joinPublicURL(baseURL, playPath string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return playPath
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/") + playPath
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + playPath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func joinPublicURLWithQuery(baseURL, playPath string, query url.Values) string {
	u := &url.URL{Path: playPath, RawQuery: query.Encode()}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return u.String()
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(baseURL, "/") + u.String()
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + playPath
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func joinPublicURLReadable(baseURL, playPath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	playPath = "/" + strings.TrimLeft(playPath, "/")
	playPath = escapeReadablePath(playPath)
	if baseURL == "" {
		return playPath
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL + playPath
	}
	prefix := parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.Path, "/")
	return prefix + playPath
}

func escapeReadablePath(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '/':
			builder.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r >= 0x80:
			builder.WriteRune(r)
		case strings.ContainsRune("-._~!$&'()*+,;=:@", r):
			builder.WriteRune(r)
		default:
			for _, b := range []byte(string(r)) {
				builder.WriteString(fmt.Sprintf("%%%02X", b))
			}
		}
	}
	return builder.String()
}

func cleanPlayDisplayName(displayName string) string {
	displayName = strings.Trim(strings.ReplaceAll(displayName, "\\", "/"), "/")
	if decoded, err := url.PathUnescape(displayName); err == nil {
		displayName = decoded
	}
	parts := splitRelativePath(displayName)
	if len(parts) == 0 {
		return ""
	}
	for _, part := range parts {
		if part == ".." {
			return ""
		}
	}
	return path.Join(parts...)
}

func playPathCandidates(baseURL, route string) []string {
	route = cleanPlayDisplayName(route)
	if route == "" {
		return nil
	}
	playPath := "/play/115/" + route
	escapedPath := (&url.URL{Path: playPath}).String()
	values := []string{
		joinPublicURLReadable(baseURL, playPath),
		joinPublicURL(baseURL, playPath),
		playPath,
		escapedPath,
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func newCodeChallenge() (string, string, error) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := hex.EncodeToString(raw)
	if len(verifier) > 128 {
		verifier = verifier[:128]
	}
	if len(verifier) < 43 {
		return "", "", errors.New("PKCE code verifier 生成失败")
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.StdEncoding.EncodeToString(sum[:]), nil
}

func randomToken(size int) (string, error) {
	if size <= 0 {
		size = 18
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
