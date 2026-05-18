package p115

import (
	"bytes"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"curio/internal/models"
	"curio/internal/playdiag"
	"curio/internal/repository"

	"golang.org/x/sync/singleflight"
)

type Service struct {
	store         *repository.Store
	syncMu        sync.Mutex
	cacheMu       sync.Mutex
	directCache   map[string]cachedDirectURL
	directGroup   singleflight.Group
	authMu        sync.Mutex
	qrSessions    map[string]qrAuthSession
	oauthSessions map[string]oauthSession
}

type cachedDirectURL struct {
	URL       string
	ExpiresAt time.Time
}

type directResolveResult struct {
	URL    string
	Source string
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

const (
	defaultDirectURLTTL       = 50 * time.Minute
	legacyDirectURLTTLSeconds = 300
)

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
	return s.runLogged(ctx, models.P115SyncTriggerManualExport, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.exportTree(ctx)
	})
}

func (s *Service) exportTree(ctx context.Context) (models.STRMSyncResult, error) {
	settings, lib, err := s.settingsAndLibrary(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "export"}
	items, err := client.ExportTree(ctx, lib)
	if err != nil {
		result.Failed++
		return result, err
	}
	items, snapshot := prepareTreeItems(lib, items, result.TreeVersion)
	if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, result.TreeVersion, snapshot); err != nil {
		return result, err
	}
	result.Exported += len(items)
	result.Skipped += countMediaTreeItems(items)
	result.EventSummary = exportTreeSummary(items)
	return result, nil
}

func (s *Service) Sync(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerManualSync, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.sync(ctx, fallbackBaseURL)
	})
}

func (s *Service) SyncScheduled(ctx context.Context) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerCron, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.sync(ctx, "")
	})
}

func (s *Service) sync(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	settings, lib, err := s.settingsAndLibrary(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	client := NewClient(settings)
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "export"}
	items, err := client.ExportTree(ctx, lib)
	if err != nil {
		result.Failed++
		return result, err
	}
	result.EventSummary = exportTreeSummary(items)
	if err := s.applySTRMItems(ctx, settings, fallbackBaseURL, lib, items, result.TreeVersion, "export", true, &result); err != nil {
		return result, err
	}
	if settings.RefreshEmbyAfterSync {
		_ = refreshEmby(ctx, settings)
	}
	return result, nil
}

func (s *Service) RebuildNodes(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerRebuildNodes, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.rebuildNodes(ctx, fallbackBaseURL)
	})
}

func (s *Service) rebuildNodes(ctx context.Context, fallbackBaseURL string) (models.STRMSyncResult, error) {
	return s.sync(ctx, fallbackBaseURL)
}

func (s *Service) Cleanup(ctx context.Context) (models.STRMSyncResult, error) {
	return s.runLogged(ctx, models.P115SyncTriggerManualCleanup, func(ctx context.Context) (models.STRMSyncResult, error) {
		return s.cleanup(ctx)
	})
}

func (s *Service) cleanup(ctx context.Context) (models.STRMSyncResult, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	links, err := s.store.STRMLinksByStatuses(ctx, []string{models.STRMStatusStale, models.STRMStatusDeleted, models.STRMStatusFailed})
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	result := models.STRMSyncResult{TreeVersion: treeVersion(), Mode: "cleanup"}
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

func (s *Service) SyncRuns(ctx context.Context, limit int) ([]models.P115SyncRun, error) {
	return s.store.P115SyncRuns(ctx, limit)
}

func (s *Service) StartScheduler(ctx context.Context) {
	go s.scheduler(ctx)
}

func (s *Service) scheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScheduledIfDue(ctx)
		}
	}
}

func (s *Service) runScheduledIfDue(ctx context.Context) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil || !settings.SyncCronEnabled {
		return
	}
	interval := time.Duration(settings.SyncIntervalMinutes) * time.Minute
	if interval < 5*time.Minute {
		interval = 5 * time.Minute
	}
	last, ok, err := s.store.LatestP115SyncRun(ctx, []string{models.P115SyncTriggerManualSync, models.P115SyncTriggerCron})
	if err != nil {
		return
	}
	if ok && time.Since(last.StartedAt) < interval {
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	_, _ = s.SyncScheduled(runCtx)
}

func (s *Service) runLogged(ctx context.Context, trigger string, fn func(context.Context) (models.STRMSyncResult, error)) (models.STRMSyncResult, error) {
	if !s.syncMu.TryLock() {
		return models.STRMSyncResult{}, errors.New("115 同步正在运行，请稍后再试")
	}
	defer s.syncMu.Unlock()

	id, err := randomToken(12)
	if err != nil {
		return models.STRMSyncResult{}, err
	}
	started := time.Now()
	run := models.P115SyncRun{
		ID:        trigger + "_" + id,
		Trigger:   trigger,
		Status:    models.P115SyncStatusRunning,
		StartedAt: started,
	}
	if err := s.store.CreateP115SyncRun(ctx, run); err != nil {
		return models.STRMSyncResult{}, err
	}

	result, runErr := fn(ctx)
	ended := time.Now()
	run.Mode = result.Mode
	run.TreeVersion = result.TreeVersion
	run.Exported = result.Exported
	run.Generated = result.Generated
	run.Restored = result.Restored
	run.Updated = result.Updated
	run.Deleted = result.Deleted
	run.Skipped = result.Skipped
	run.Failed = result.Failed
	run.EventSummary = result.EventSummary
	run.EndedAt = &ended
	run.DurationMS = ended.Sub(started).Milliseconds()
	switch {
	case runErr != nil:
		run.Status = models.P115SyncStatusFailed
		run.ErrorMessage = runErr.Error()
	case result.Failed > 0:
		run.Status = models.P115SyncStatusPartial
	default:
		run.Status = models.P115SyncStatusSuccess
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.FinishP115SyncRun(finishCtx, run); err != nil && runErr == nil {
		return result, err
	}
	return result, runErr
}

func (s *Service) PlayURLForLink(linkID, baseURL string) (string, error) {
	token, err := signPlayToken(linkID, 10*365*24*time.Hour)
	if err != nil {
		return "", err
	}
	return joinPublicURL(baseURL, "/play/115/"+token), nil
}

func (s *Service) PlayURLForLinkName(linkID, baseURL, displayName string) (string, error) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return "", errors.New("STRM 链接 ID 为空")
	}
	return joinPublicURLReadable(baseURL, "/play/115/id/"+linkID+"/"+playRouteFileName(linkID, displayName)), nil
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
	if linkID := linkIDFromPlayRoute(route); linkID != "" {
		return s.resolvePlayURLByLinkID(ctx, linkID, requestUserAgent)
	}
	settings, lib, err := s.settingsAndLibrary(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLinkByPlayRoute(ctx, models.STRMProvider115, route, playPathCandidatesForBases(route, baseURL, settings.PublicBaseURL))
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, lib, link, requestUserAgent)
}

func (s *Service) resolvePlayURLByLinkID(ctx context.Context, linkID, requestUserAgent string) (string, error) {
	settings, lib, err := s.settingsAndLibrary(ctx)
	if err != nil {
		return "", err
	}
	link, err := s.store.STRMLink(ctx, linkID)
	if err != nil {
		return "", err
	}
	return s.resolvePlayURLForLink(ctx, settings, lib, link, requestUserAgent)
}

func (s *Service) resolvePlayURLForLink(ctx context.Context, settings models.P115Settings, lib LibraryConfig, link models.STRMLink, requestUserAgent string) (string, error) {
	started := time.Now()
	if link.Provider != models.STRMProvider115 || link.Status != models.STRMStatusGenerated {
		return "", errors.New("STRM 链接不可播放")
	}
	if link.LibraryCID != lib.CID {
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
		logDirectResolve(link, requestUserAgent, ua, "cache-hit", false, directURL, time.Since(started), "")
		return directURL, nil
	}
	cacheKey := directCacheKey(link.PickCode, ua)
	value, err, shared := s.directGroup.Do(cacheKey, func() (any, error) {
		if directURL, ok := s.cachedDirectURL(link.PickCode, ua); ok {
			return directResolveResult{URL: directURL, Source: "cache-hit-after-wait"}, nil
		}
		directURL, err := client.DirectURL(ctx, link.PickCode, ua)
		if err != nil {
			_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, link.Status, models.STRMResolveFailed, "DIRECT_URL_FAILED", err.Error())
			return directResolveResult{}, err
		}
		s.rememberDirectURL(link.PickCode, ua, directURL, directURLTTL(settings))
		return directResolveResult{URL: directURL, Source: "115-api"}, nil
	})
	if err != nil {
		logDirectResolve(link, requestUserAgent, ua, "failed", shared, "", time.Since(started), err.Error())
		return "", err
	}
	result, _ := value.(directResolveResult)
	directURL := result.URL
	if strings.TrimSpace(directURL) == "" {
		err := errors.New("115 未返回可用直链")
		logDirectResolve(link, requestUserAgent, ua, "empty", shared, "", time.Since(started), err.Error())
		return "", err
	}
	source := result.Source
	if source == "" {
		source = "unknown"
	}
	logDirectResolve(link, requestUserAgent, ua, source, shared, directURL, time.Since(started), "")
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

func (s *Service) settingsAndLibrary(ctx context.Context) (models.P115Settings, LibraryConfig, error) {
	settings, err := s.store.P115Settings(ctx)
	if err != nil {
		return models.P115Settings{}, LibraryConfig{}, err
	}
	if !settings.Enabled {
		return models.P115Settings{}, LibraryConfig{}, errors.New("115 播放未启用")
	}
	lib, err := ParseLibraryCID(settings.LibraryCID)
	if err != nil {
		return models.P115Settings{}, LibraryConfig{}, err
	}
	if strings.TrimSpace(settings.STRMOutputPath) == "" {
		return models.P115Settings{}, LibraryConfig{}, errors.New("STRM 输出目录不能为空")
	}
	return settings, lib, nil
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

func (s *Service) applySTRMItems(ctx context.Context, settings models.P115Settings, fallbackBaseURL string, lib LibraryConfig, items []TreeItem, version, sourceMode string, countExported bool, result *models.STRMSyncResult) error {
	items, _ = prepareTreeItems(lib, items, version)
	if countExported {
		result.Exported += len(items)
	}
	seen := map[string]struct{}{}
	desiredTargets := map[string]struct{}{}
	for _, item := range items {
		if isMediaTreeItem(item) {
			seen[item.RelativePath] = struct{}{}
		}
	}
	existing, err := s.store.ActiveSTRMLinksByLibrary(ctx, models.STRMProvider115, lib.CID)
	if err != nil {
		return err
	}
	existingByPath := make(map[string]models.STRMLink, len(existing))
	for _, link := range existing {
		existingByPath[link.RelativePath] = link
	}
	localTargets, err := localSTRMFiles(settings.STRMOutputPath, lib.OutputPrefix)
	if err != nil {
		return err
	}
	desiredRelativePaths := desiredMediaRelativePaths(items)
	if prefix, matched, total, ok := detectRelativePrefixShift(strmLinkRelativePaths(existing), desiredRelativePaths); ok {
		return fmt.Errorf("检测到 115 目录树疑似多出顶层目录 %q：%d/%d 个现有 STRM 可在该目录下匹配。已中止同步以保护现有 STRM，请确认媒体库 CID 是否指向根目录本身", prefix, matched, total)
	}
	desiredSTRMPaths := desiredSTRMRelativePaths(items)
	if prefix, matched, total, ok := detectRelativePrefixShift(localSTRMRelativePaths(settings.STRMOutputPath, lib.OutputPrefix, localTargets), desiredSTRMPaths); ok {
		return fmt.Errorf("检测到本地 STRM 目标疑似整体多出顶层目录 %q：%d/%d 个现有文件可在该目录下匹配。已中止同步以保护现有 STRM", prefix, matched, total)
	}
	removedTargets := map[string]struct{}{}
	for _, item := range items {
		if !isMediaTreeItem(item) {
			continue
		}
		link, err := s.linkForItem(settings, fallbackBaseURL, lib, item, version)
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
		targetKey := cleanPathKey(link.STRMPath)
		desiredTargets[targetKey] = struct{}{}
		_, localExists := localTargets[targetKey]
		if !localExists {
			localExists = fileExists(link.STRMPath)
		}
		wrote, err := writeSTRM(settings.STRMOutputPath, link.STRMPath, link.PlayPath)
		if err != nil {
			result.Failed++
			_ = s.store.MarkSTRMLinkStatus(ctx, link.ID, models.STRMStatusFailed, models.STRMResolveFailed, "STRM_WRITE_FAILED", err.Error())
			continue
		}
		if err := s.store.UpsertSTRMLink(ctx, link); err != nil {
			result.Failed++
			continue
		}
		if existed && old.STRMPath != "" && cleanPathKey(old.STRMPath) != targetKey {
			if err := removeManagedSTRM(settings.STRMOutputPath, old.STRMPath); err == nil {
				removedTargets[cleanPathKey(old.STRMPath)] = struct{}{}
			} else {
				result.Failed++
			}
		}
		if !existed {
			result.Generated++
		} else if old.Status != models.STRMStatusGenerated || !localExists {
			result.Restored++
		} else if wrote || old.SourceTreeHash != link.SourceTreeHash || old.STRMPath != link.STRMPath || old.PlayPath != link.PlayPath {
			result.Updated++
		} else {
			result.Skipped++
		}
	}
	if canMarkMissingFromSource(sourceMode) {
		for _, link := range existing {
			if _, ok := seen[link.RelativePath]; ok {
				continue
			}
			if err := s.markMissing(ctx, settings, link); err != nil {
				result.Failed++
				continue
			}
			removedTargets[cleanPathKey(link.STRMPath)] = struct{}{}
			result.Deleted++
		}
	}
	if settings.DeleteMissingSTRM {
		for targetKey, target := range localTargets {
			if _, ok := desiredTargets[targetKey]; ok {
				continue
			}
			if _, ok := removedTargets[targetKey]; ok {
				continue
			}
			if err := removeManagedSTRM(settings.STRMOutputPath, target); err != nil {
				result.Failed++
				continue
			}
			result.Deleted++
		}
	}
	if sourceMode == "export" {
		_, snapshot := prepareTreeItems(lib, items, version)
		if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, version, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) rebuildLibraryNodesFromScan(ctx context.Context, client *Client, lib LibraryConfig, version string) ([]TreeItem, error) {
	items, err := client.ScanTree(ctx, lib)
	if err != nil {
		return nil, err
	}
	items, snapshot := prepareTreeItems(lib, items, version)
	if !treeItemsHaveRemoteIdentity(items) {
		return nil, errors.New("115 分页扫描未返回可用于重建节点的 file_id")
	}
	cursor := latestCursor(ctx, client, lib.CID, "rebuild_nodes")
	if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, version, nodesFromTreeItems(lib, items, version), cursor); err != nil {
		return nil, err
	}
	if err := s.store.ReplaceP115Snapshot(ctx, lib.CID, version, snapshot); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) enrichSTRMLinksFromTreeItems(ctx context.Context, settings models.P115Settings, fallbackBaseURL string, lib LibraryConfig, items []TreeItem, version string) error {
	for _, item := range items {
		if !isMediaTreeItem(item) {
			continue
		}
		link, err := s.linkForItem(settings, fallbackBaseURL, lib, item, version)
		if err != nil {
			return err
		}
		if !fileExists(link.STRMPath) {
			continue
		}
		if err := s.store.UpsertSTRMLink(ctx, link); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) treeItemsForSync(ctx context.Context, client *Client, lib LibraryConfig, version string) ([]TreeItem, string, string, string, error) {
	nodeCount, err := s.store.P115NodeCount(ctx, lib.CID)
	if err != nil {
		return nil, "", "", "", err
	}
	eventSummary := ""
	if nodeCount > 0 {
		nodes, nodeVersion, eventMode, eventSummary, err := s.nodesWithLifeEvents(ctx, client, lib, version)
		if err == nil {
			if nodeVersion != "" {
				version = nodeVersion
			}
			return treeItemsFromNodes(nodes), version, eventMode, eventSummary, nil
		}
	}
	items, scanErr := client.ExportTree(ctx, lib)
	if scanErr == nil {
		items, _ = prepareTreeItems(lib, items, version)
		sourceMode := "export"
		if treeItemsHaveRemoteIdentity(items) {
			sourceMode = "scan"
			nodes := nodesFromTreeItems(lib, items, version)
			if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, version, nodes, latestCursor(ctx, client, lib.CID, "scan")); err != nil {
				return nil, "", "", "", err
			}
		}
		return items, version, sourceMode, eventSummary, nil
	}
	if nodeCount > 0 {
		nodes, nodeVersion, err := s.store.P115Nodes(ctx, lib.CID, true)
		if err != nil {
			return nil, "", "", "", err
		}
		if nodeVersion != "" {
			version = nodeVersion
		}
		return treeItemsFromNodes(nodes), version, "cache", eventSummary, nil
	}
	snapshot, snapshotVersion, err := s.store.P115Snapshot(ctx, lib.CID)
	if err != nil {
		return nil, "", "", "", err
	}
	if len(snapshot) > 0 {
		return treeItemsFromSnapshot(snapshot), snapshotVersion, "snapshot", eventSummary, nil
	}
	return nil, "", "", "", scanErr
}

func (s *Service) nodesWithLifeEvents(ctx context.Context, client *Client, lib LibraryConfig, version string) ([]models.P115Node, string, string, string, error) {
	nodes, nodeVersion, err := s.store.P115Nodes(ctx, lib.CID, false)
	if err != nil {
		return nil, "", "", "", err
	}
	cursor, err := s.store.P115EventCursor(ctx, lib.CID)
	if err != nil {
		return nil, "", "", "", err
	}
	if cursor.LastEventID == 0 && cursor.LastEventTime == 0 {
		cursor = latestCursor(ctx, client, lib.CID, "init")
		summary := cursorSummary("init", LifeEventBatch{}, cursor, cursor, "")
		if cursor.LastSyncStatus == "error" {
			if err := s.store.SaveP115EventCursor(ctx, cursor); err != nil {
				return nil, "", "", summary, err
			}
			return nil, "", "", summary, errors.New(cursor.LastSyncError)
		}
		if err := s.store.SaveP115EventCursor(ctx, cursor); err != nil {
			return nil, "", "", summary, err
		}
		return aliveNodes(nodes), nodeVersion, "events", summary, nil
	}
	batch, err := client.LifeEventsBatch(ctx, cursor.LastEventID, cursor.LastEventTime, 20)
	if err != nil {
		summary := fmt.Sprintf("event_error from_id=%d from_time=%d err=%s", cursor.LastEventID, cursor.LastEventTime, err.Error())
		saveErr := s.store.SaveP115EventCursor(ctx, models.P115EventCursor{
			LibraryCID:     lib.CID,
			LastEventID:    cursor.LastEventID,
			LastEventTime:  cursor.LastEventTime,
			LastSyncStatus: "error",
			LastSyncError:  err.Error(),
		})
		if saveErr != nil {
			return nil, "", "", summary, saveErr
		}
		return nil, "", "", summary, err
	}
	events := batch.Events
	nextCursor := advanceCursorWithBatch(cursor, batch)
	nextCursor.LibraryCID = lib.CID
	nextCursor.LastSyncStatus = "ok"
	nextCursor.LastSyncError = ""
	eventSummary := eventBatchSummary(batch, cursor, nextCursor)
	if len(events) == 0 {
		if err := s.store.SaveP115EventCursor(ctx, nextCursor); err != nil {
			return nil, "", "", eventSummary, err
		}
		return aliveNodes(nodes), nodeVersion, "events", eventSummary, nil
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, right := eventApplyPriority(events[i]), eventApplyPriority(events[j])
		if left != right {
			return left < right
		}
		return events[i].ID < events[j].ID
	})
	nextVersion := version
	if nextVersion == "" {
		nextVersion = treeVersion()
	}
	updated, changed := applyLifeEventsToNodes(lib, nodes, events, nextVersion)
	parentScanUsed := false
	if eventsNeedParentDirectoryReconcile(nodes, events) {
		var parentChanged bool
		updated, parentChanged, err = s.reconcileEventDirectoriesFromParents(ctx, client, lib, updated, nodes, events, nextVersion)
		if err != nil {
			saveErr := s.store.SaveP115EventCursor(ctx, models.P115EventCursor{
				LibraryCID:     lib.CID,
				LastEventID:    cursor.LastEventID,
				LastEventTime:  cursor.LastEventTime,
				LastSyncStatus: "error",
				LastSyncError:  err.Error(),
			})
			if saveErr != nil {
				return nil, "", "", eventSummary, saveErr
			}
			return nil, "", "", eventSummary, err
		}
		parentScanUsed = true
		changed = changed || parentChanged
		eventSummary = appendSummary(eventSummary, fmt.Sprintf("parent_scan=%d", eventParentDirectoryReconcileCount(nodes, events)))
	}
	eventMode := "events"
	if parentScanUsed {
		eventMode = "events_parent_scan"
	}
	if !parentScanUsed && eventsNeedTreeScan(events) {
		var scanChanged bool
		subtreeScans := len(eventSubtreeScanRoots(updated, events))
		updated, scanChanged, err = s.scanEventSubtrees(ctx, client, lib, updated, events, nextVersion)
		if err != nil {
			saveErr := s.store.SaveP115EventCursor(ctx, models.P115EventCursor{
				LibraryCID:     lib.CID,
				LastEventID:    cursor.LastEventID,
				LastEventTime:  cursor.LastEventTime,
				LastSyncStatus: "error",
				LastSyncError:  err.Error(),
			})
			if saveErr != nil {
				return nil, "", "", eventSummary, saveErr
			}
			return nil, "", "", eventSummary, err
		}
		changed = changed || scanChanged
		eventSummary = appendSummary(eventSummary, fmt.Sprintf("subtree_scan=%d", subtreeScans))
	}
	if changed {
		if err := s.store.ReplaceP115NodesAndCursor(ctx, lib.CID, nextVersion, updated, nextCursor); err != nil {
			return nil, "", "", eventSummary, err
		}
		return aliveNodes(updated), nextVersion, eventMode, eventSummary, nil
	}
	if err := s.store.SaveP115EventCursor(ctx, nextCursor); err != nil {
		return nil, "", "", eventSummary, err
	}
	return aliveNodes(updated), nodeVersion, eventMode, eventSummary, nil
}

func latestCursor(ctx context.Context, client *Client, libraryCID, status string) models.P115EventCursor {
	id, eventTime, err := client.LatestLifeEventCursor(ctx)
	cursor := models.P115EventCursor{
		LibraryCID:     libraryCID,
		LastEventID:    id,
		LastEventTime:  eventTime,
		LastSyncStatus: status,
	}
	if err != nil {
		cursor.LastSyncStatus = "error"
		cursor.LastSyncError = err.Error()
	}
	return cursor
}

func eventsNeedTreeScan(events []LifeEvent) bool {
	for _, event := range events {
		if eventNeedsSubtreeScan(event) {
			return true
		}
	}
	return false
}

func eventNeedsSubtreeScan(event LifeEvent) bool {
	return eventCreatesDirectory(event)
}

func eventBatchSummary(batch LifeEventBatch, cursor, nextCursor models.P115EventCursor) string {
	parts := []string{
		fmt.Sprintf("source=%s", valueOr(batch.Source, "unknown")),
		fmt.Sprintf("raw=%d", batch.RawCount),
		fmt.Sprintf("events=%d", len(batch.Events)),
		fmt.Sprintf("cursor=%d/%d->%d/%d", cursor.LastEventID, cursor.LastEventTime, nextCursor.LastEventID, nextCursor.LastEventTime),
	}
	if len(batch.Events) > 0 {
		parts = append(parts,
			"types="+eventTypeSummary(batch.Events),
			fmt.Sprintf("folders=%d", countDirectoryEvents(batch.Events)),
			"samples="+eventSampleSummary(batch.Events, 4),
		)
	}
	return strings.Join(parts, " ")
}

func cursorSummary(source string, batch LifeEventBatch, cursor, nextCursor models.P115EventCursor, extra string) string {
	if batch.Source == "" {
		batch.Source = source
	}
	summary := eventBatchSummary(batch, cursor, nextCursor)
	if extra != "" {
		summary = appendSummary(summary, extra)
	}
	return summary
}

func eventTypeSummary(events []LifeEvent) string {
	counts := map[string]int{}
	for _, event := range events {
		name := strings.TrimSpace(event.EventName)
		if name == "" {
			name = behaviorTypeNames[event.Type]
		}
		if name == "" {
			name = strconv.Itoa(event.Type)
		}
		counts[name]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func countDirectoryEvents(events []LifeEvent) int {
	count := 0
	for _, event := range events {
		if eventCreatesDirectory(event) {
			count++
		}
	}
	return count
}

func eventSampleSummary(events []LifeEvent, limit int) string {
	if limit <= 0 || len(events) == 0 {
		return ""
	}
	if limit > len(events) {
		limit = len(events)
	}
	parts := make([]string, 0, limit)
	for _, event := range events[:limit] {
		name := strings.TrimSpace(event.Name)
		if name == "" {
			name = "-"
		}
		metadata := make([]string, 0, 3)
		if event.PickCode != "" {
			metadata = append(metadata, "pc")
		}
		if event.SHA1 != "" {
			metadata = append(metadata, "sha")
		}
		if event.Size > 0 {
			metadata = append(metadata, "size")
		}
		if len(metadata) == 0 {
			metadata = append(metadata, "no-meta")
		}
		parts = append(parts, fmt.Sprintf("%s[%s]", shortSummaryToken(name, 18), strings.Join(metadata, "+")))
	}
	return strings.Join(parts, ",")
}

func shortSummaryToken(value string, limit int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), " ", "_")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

func appendSummary(current, next string) string {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" {
		return next
	}
	if next == "" {
		return current
	}
	return current + " | " + next
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func eventsNeedParentDirectoryReconcile(nodes []models.P115Node, events []LifeEvent) bool {
	existing := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.RemoteFileID != "" && node.IsAlive {
			existing[node.RemoteFileID] = struct{}{}
		}
	}
	for _, event := range events {
		if !eventCreatesDirectory(event) || event.FileID == "" {
			continue
		}
		if _, ok := existing[event.FileID]; ok {
			continue
		}
		switch event.Type {
		case 2, 5, 6, 14, 18, 23:
			return true
		}
	}
	return false
}

func eventApplyPriority(event LifeEvent) int {
	if eventCreatesDirectory(event) {
		return 0
	}
	return 1
}

func eventCreatesDirectory(event LifeEvent) bool {
	switch event.Type {
	case 17, 18, 20:
		return true
	case 2, 5, 6, 14, 23:
		return eventNameLooksDirectory(event.Name) || (strings.TrimSpace(event.Name) == "" && strings.TrimSpace(event.PickCode) == "" && strings.TrimSpace(event.SHA1) == "" && event.Size == 0)
	default:
		return false
	}
}

func eventNameLooksDirectory(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if ext == "" {
		return true
	}
	if mediaExtension("." + ext) {
		return false
	}
	if _, ok := nonDirectoryEventExtensions[ext]; ok {
		return false
	}
	return true
}

var nonDirectoryEventExtensions = map[string]struct{}{
	"ass": {}, "srt": {}, "ssa": {}, "vtt": {},
	"jpg": {}, "jpeg": {}, "png": {}, "gif": {}, "webp": {}, "bmp": {},
	"nfo": {}, "txt": {}, "log": {}, "pdf": {},
	"zip": {}, "rar": {}, "7z": {}, "tar": {}, "gz": {},
}

func advanceCursor(cursor models.P115EventCursor, events []LifeEvent) models.P115EventCursor {
	for _, event := range events {
		if event.ID > cursor.LastEventID {
			cursor.LastEventID = event.ID
		}
		if event.UpdateTime > cursor.LastEventTime {
			cursor.LastEventTime = event.UpdateTime
		}
	}
	return cursor
}

func advanceCursorWithBatch(cursor models.P115EventCursor, batch LifeEventBatch) models.P115EventCursor {
	if batch.LastEventID > cursor.LastEventID {
		cursor.LastEventID = batch.LastEventID
	}
	if batch.LastEventTime > cursor.LastEventTime {
		cursor.LastEventTime = batch.LastEventTime
	}
	if batch.LastEventID == 0 && batch.LastEventTime == 0 {
		return advanceCursor(cursor, batch.Events)
	}
	return cursor
}

func applyLifeEventsToNodes(lib LibraryConfig, nodes []models.P115Node, events []LifeEvent, version string) ([]models.P115Node, bool) {
	byID := make(map[string]*models.P115Node, len(nodes)+len(events))
	out := make([]models.P115Node, 0, len(nodes)+len(events))
	for _, node := range nodes {
		if strings.TrimSpace(node.RemoteFileID) == "" {
			continue
		}
		node.LibraryCID = lib.CID
		out = append(out, node)
		byID[node.RemoteFileID] = &out[len(out)-1]
	}
	changed := false
	for _, event := range events {
		if event.FileID == "" {
			continue
		}
		node, exists := byID[event.FileID]
		if event.Type == 22 {
			if exists && node.IsAlive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
			}
			continue
		}
		parentInside := event.ParentID == lib.CID
		if !parentInside {
			if parent, ok := byID[event.ParentID]; ok && parent.IsAlive {
				parentInside = true
			}
		}
		if exists && !parentInside {
			if node.IsAlive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
			}
			continue
		}
		if !parentInside {
			continue
		}
		name := strings.TrimSpace(event.Name)
		if name == "" && exists {
			name = node.Name
		}
		if name == "" {
			continue
		}
		if !exists {
			newNode := models.P115Node{LibraryCID: lib.CID, RemoteFileID: event.FileID}
			out = append(out, newNode)
			node = &out[len(out)-1]
			byID[event.FileID] = node
		}
		node.LibraryCID = lib.CID
		node.TreeVersion = version
		node.ParentFileID = event.ParentID
		node.Name = name
		node.IsAlive = true
		node.IsDirectory = eventIsDirectory(event, *node)
		if event.PickCode != "" {
			node.PickCode = event.PickCode
		}
		if event.SHA1 != "" || node.IsDirectory {
			node.SHA1 = event.SHA1
		}
		if event.Size > 0 || node.IsDirectory {
			node.Size = event.Size
		}
		changed = true
	}
	if rebuildNodePaths(lib, out, version) {
		changed = true
	}
	return out, changed
}

func (s *Service) scanEventSubtrees(ctx context.Context, client *Client, lib LibraryConfig, nodes []models.P115Node, events []LifeEvent, version string) ([]models.P115Node, bool, error) {
	roots := eventSubtreeScanRoots(nodes, events)
	changed := false
	for _, root := range roots {
		items, err := client.ScanSubtree(ctx, lib, root.RemoteFileID, root.RelativePath, pathDepth(root.RelativePath))
		if err != nil {
			return nodes, changed, fmt.Errorf("扫描 115 目录 %s 失败：%w", root.RelativePath, err)
		}
		var subtreeChanged bool
		nodes, subtreeChanged = mergeScannedSubtree(lib, nodes, root, items, version)
		changed = changed || subtreeChanged
	}
	return nodes, changed, nil
}

func (s *Service) reconcileEventDirectoriesFromParents(ctx context.Context, client *Client, lib LibraryConfig, nodes, baseline []models.P115Node, events []LifeEvent, version string) ([]models.P115Node, bool, error) {
	candidates := eventParentDirectoryReconcileCandidates(baseline, events)
	if len(candidates) == 0 {
		return nodes, false, nil
	}
	changed := false
	for _, event := range candidates {
		root, ok, err := s.resolveEventDirectoryFromParent(ctx, client, lib, nodes, event, version)
		if err != nil {
			return nodes, changed, err
		}
		if !ok {
			return nodes, changed, fmt.Errorf("未能在父目录中定位 115 目录：%s", strings.TrimSpace(event.Name))
		}
		if root.RemoteFileID != event.FileID {
			var superseded bool
			nodes, superseded = markSupersededEventDirectory(nodes, event, version)
			changed = changed || superseded
		}
		items, err := client.ScanSubtree(ctx, lib, root.RemoteFileID, root.RelativePath, pathDepth(root.RelativePath))
		if err != nil {
			return nodes, changed, fmt.Errorf("扫描 115 目录 %s 失败：%w", root.RelativePath, err)
		}
		var subtreeChanged bool
		nodes, subtreeChanged = mergeScannedSubtree(lib, nodes, root, items, version)
		changed = changed || subtreeChanged
	}
	return nodes, changed, nil
}

func eventParentDirectoryReconcileCandidates(nodes []models.P115Node, events []LifeEvent) []LifeEvent {
	existing := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.RemoteFileID != "" && node.IsAlive {
			existing[node.RemoteFileID] = struct{}{}
		}
	}
	out := make([]LifeEvent, 0)
	seen := map[string]struct{}{}
	for _, event := range events {
		if !eventCreatesDirectory(event) || event.FileID == "" || strings.TrimSpace(event.Name) == "" {
			continue
		}
		if _, ok := existing[event.FileID]; ok {
			continue
		}
		switch event.Type {
		case 2, 5, 6, 14, 18, 23:
		default:
			continue
		}
		key := strings.TrimSpace(event.ParentID) + "\x00" + strings.TrimSpace(event.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, event)
	}
	return out
}

func eventParentDirectoryReconcileCount(nodes []models.P115Node, events []LifeEvent) int {
	return len(eventParentDirectoryReconcileCandidates(nodes, events))
}

func (s *Service) resolveEventDirectoryFromParent(ctx context.Context, client *Client, lib LibraryConfig, nodes []models.P115Node, event LifeEvent, version string) (models.P115Node, bool, error) {
	parent, ok := eventParentDirectory(lib, nodes, event)
	if !ok {
		return models.P115Node{}, false, fmt.Errorf("115 目录事件父节点不在当前节点缓存：%s", strings.TrimSpace(event.ParentID))
	}
	children, err := client.List(ctx, parent.RemoteFileID)
	if err != nil {
		return models.P115Node{}, false, fmt.Errorf("读取 115 父目录 %s 失败：%w", parent.RelativePath, err)
	}
	name := strings.TrimSpace(event.Name)
	for _, child := range children {
		if !child.IsDirectory || child.Name != name || child.ID == "" {
			continue
		}
		rel := child.Name
		if parent.RelativePath != "" {
			rel = path.Join(parent.RelativePath, child.Name)
		}
		item := TreeItem{
			RelativePath: rel,
			Name:         child.Name,
			RemoteFileID: child.ID,
			ParentFileID: parent.RemoteFileID,
			PickCode:     child.PickCode,
			SHA1:         child.SHA1,
			Size:         child.Size,
			Depth:        pathDepth(rel),
			IsDirectory:  true,
		}
		item.SourceTreeHash = sourceHash(lib.CID, item.RelativePath, item.RemoteFileID, item.PickCode, item.SHA1, item.Size)
		return nodeFromTreeItem(lib, item, version), true, nil
	}
	return models.P115Node{}, false, nil
}

func eventParentDirectory(lib LibraryConfig, nodes []models.P115Node, event LifeEvent) (models.P115Node, bool) {
	parentID := strings.TrimSpace(event.ParentID)
	if parentID == "" || parentID == lib.CID {
		return models.P115Node{LibraryCID: lib.CID, RemoteFileID: lib.CID, IsDirectory: true, IsAlive: true}, true
	}
	for _, node := range nodes {
		if node.RemoteFileID == parentID && node.IsAlive && node.IsDirectory {
			return node, true
		}
	}
	return models.P115Node{}, false
}

func markSupersededEventDirectory(nodes []models.P115Node, event LifeEvent, version string) ([]models.P115Node, bool) {
	changed := false
	for i := range nodes {
		if nodes[i].RemoteFileID != event.FileID || !nodes[i].IsAlive || !nodes[i].IsDirectory {
			continue
		}
		nodes[i].IsAlive = false
		nodes[i].TreeVersion = version
		changed = true
	}
	return nodes, changed
}

func eventSubtreeScanRoots(nodes []models.P115Node, events []LifeEvent) []models.P115Node {
	byID := make(map[string]int, len(nodes))
	for i := range nodes {
		if nodes[i].RemoteFileID != "" {
			byID[nodes[i].RemoteFileID] = i
		}
	}
	seen := map[string]struct{}{}
	roots := make([]models.P115Node, 0)
	for _, event := range events {
		if !eventNeedsSubtreeScan(event) || event.FileID == "" {
			continue
		}
		index, ok := byID[event.FileID]
		if !ok {
			continue
		}
		node := nodes[index]
		if !node.IsAlive || !node.IsDirectory || node.RelativePath == "" {
			continue
		}
		if _, ok := seen[node.RemoteFileID]; ok {
			continue
		}
		seen[node.RemoteFileID] = struct{}{}
		roots = append(roots, node)
	}
	sort.SliceStable(roots, func(i, j int) bool {
		left, right := pathDepth(roots[i].RelativePath), pathDepth(roots[j].RelativePath)
		if left != right {
			return left < right
		}
		return roots[i].RelativePath < roots[j].RelativePath
	})
	filtered := make([]models.P115Node, 0, len(roots))
	for _, root := range roots {
		nested := false
		for _, parent := range filtered {
			if nodeDescendsFrom(root.RemoteFileID, parent.RemoteFileID, nodes, byID) {
				nested = true
				break
			}
		}
		if !nested {
			filtered = append(filtered, root)
		}
	}
	return filtered
}

func mergeScannedSubtree(lib LibraryConfig, nodes []models.P115Node, root models.P115Node, items []TreeItem, version string) ([]models.P115Node, bool) {
	items, _ = prepareTreeItems(lib, items, version)
	out := make([]models.P115Node, 0, len(nodes)+len(items)+1)
	byID := make(map[string]int, len(nodes)+len(items))
	for _, node := range nodes {
		if strings.TrimSpace(node.RemoteFileID) == "" {
			continue
		}
		node.LibraryCID = lib.CID
		out = append(out, node)
		byID[node.RemoteFileID] = len(out) - 1
	}
	changed := false
	if root.RemoteFileID != "" {
		root.LibraryCID = lib.CID
		root.TreeVersion = version
		root.IsAlive = true
		root.IsDirectory = true
		root.IsMedia = false
		if root.SourceTreeHash == "" {
			root.SourceTreeHash = sourceHash(lib.CID, root.RelativePath, root.RemoteFileID, root.PickCode, root.SHA1, root.Size)
		}
		if index, ok := byID[root.RemoteFileID]; ok {
			before := out[index]
			out[index].TreeVersion = root.TreeVersion
			out[index].ParentFileID = root.ParentFileID
			out[index].RelativePath = root.RelativePath
			out[index].Name = root.Name
			out[index].PickCode = root.PickCode
			out[index].SHA1 = root.SHA1
			out[index].Size = root.Size
			out[index].IsDirectory = root.IsDirectory
			out[index].IsMedia = root.IsMedia
			out[index].IsAlive = true
			out[index].SourceTreeHash = root.SourceTreeHash
			if p115NodeChanged(before, out[index]) {
				changed = true
			}
		} else {
			out = append(out, root)
			byID[root.RemoteFileID] = len(out) - 1
			changed = true
		}
	}
	scannedIDs := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.RemoteFileID != "" {
			scannedIDs[item.RemoteFileID] = struct{}{}
		}
	}
	for i := range out {
		if out[i].RemoteFileID == root.RemoteFileID || !out[i].IsAlive {
			continue
		}
		if !nodeDescendsFrom(out[i].RemoteFileID, root.RemoteFileID, out, byID) {
			continue
		}
		if _, ok := scannedIDs[out[i].RemoteFileID]; ok {
			continue
		}
		out[i].IsAlive = false
		out[i].TreeVersion = version
		changed = true
	}
	for _, item := range items {
		node := nodeFromTreeItem(lib, item, version)
		if node.RemoteFileID == "" {
			continue
		}
		if index, ok := byID[node.RemoteFileID]; ok {
			before := out[index]
			out[index].TreeVersion = node.TreeVersion
			out[index].ParentFileID = node.ParentFileID
			out[index].RelativePath = node.RelativePath
			out[index].Name = node.Name
			out[index].PickCode = node.PickCode
			out[index].SHA1 = node.SHA1
			out[index].Size = node.Size
			out[index].IsDirectory = node.IsDirectory
			out[index].IsMedia = node.IsMedia
			out[index].IsAlive = true
			out[index].SourceTreeHash = node.SourceTreeHash
			if p115NodeChanged(before, out[index]) {
				changed = true
			}
			continue
		}
		out = append(out, node)
		byID[node.RemoteFileID] = len(out) - 1
		changed = true
	}
	if rebuildNodePaths(lib, out, version) {
		changed = true
	}
	return out, changed
}

func nodeFromTreeItem(lib LibraryConfig, item TreeItem, version string) models.P115Node {
	isMedia := mediaExtension(path.Ext(item.Name)) && !item.IsDirectory
	return models.P115Node{
		LibraryCID:     lib.CID,
		TreeVersion:    version,
		RemoteFileID:   item.RemoteFileID,
		ParentFileID:   item.ParentFileID,
		RelativePath:   item.RelativePath,
		Name:           item.Name,
		PickCode:       item.PickCode,
		SHA1:           item.SHA1,
		Size:           item.Size,
		IsDirectory:    item.IsDirectory,
		IsMedia:        isMedia,
		IsAlive:        true,
		SourceTreeHash: item.SourceTreeHash,
	}
}

func p115NodeChanged(left, right models.P115Node) bool {
	return left.TreeVersion != right.TreeVersion ||
		left.ParentFileID != right.ParentFileID ||
		left.RelativePath != right.RelativePath ||
		left.Name != right.Name ||
		left.PickCode != right.PickCode ||
		left.SHA1 != right.SHA1 ||
		left.Size != right.Size ||
		left.IsDirectory != right.IsDirectory ||
		left.IsMedia != right.IsMedia ||
		left.IsAlive != right.IsAlive ||
		left.SourceTreeHash != right.SourceTreeHash
}

func eventIsDirectory(event LifeEvent, existing models.P115Node) bool {
	if eventCreatesDirectory(event) {
		return true
	}
	if event.SHA1 != "" || event.Size > 0 {
		return false
	}
	return existing.IsDirectory
}

func nodeDescendsFrom(nodeID, rootID string, nodes []models.P115Node, byID map[string]int) bool {
	if nodeID == "" || rootID == "" || nodeID == rootID {
		return false
	}
	seen := map[string]struct{}{}
	current := nodeID
	for {
		index, ok := byID[current]
		if !ok {
			return false
		}
		parentID := nodes[index].ParentFileID
		if parentID == rootID {
			return true
		}
		if parentID == "" || parentID == current {
			return false
		}
		if _, ok := seen[parentID]; ok {
			return false
		}
		seen[parentID] = struct{}{}
		current = parentID
	}
}

func rebuildNodePaths(lib LibraryConfig, nodes []models.P115Node, version string) bool {
	byID := make(map[string]*models.P115Node, len(nodes))
	for i := range nodes {
		byID[nodes[i].RemoteFileID] = &nodes[i]
	}
	state := map[string]bool{}
	visiting := map[string]bool{}
	var changed bool
	var compute func(string) (string, bool)
	compute = func(id string) (string, bool) {
		node, ok := byID[id]
		if !ok || !node.IsAlive {
			return "", false
		}
		if alive, ok := state[id]; ok {
			return node.RelativePath, alive
		}
		if visiting[id] {
			node.IsAlive = false
			changed = true
			state[id] = false
			return "", false
		}
		visiting[id] = true
		defer delete(visiting, id)
		var rel string
		if node.ParentFileID == "" || node.ParentFileID == lib.CID {
			rel = node.Name
		} else {
			parentRel, alive := compute(node.ParentFileID)
			if !alive {
				node.IsAlive = false
				node.TreeVersion = version
				changed = true
				state[id] = false
				return "", false
			}
			rel = path.Join(parentRel, node.Name)
		}
		if node.RelativePath != rel {
			node.RelativePath = rel
			changed = true
		}
		node.IsMedia = mediaExtension(path.Ext(node.Name)) && !node.IsDirectory
		hash := sourceHash(lib.CID, node.RelativePath, node.RemoteFileID, node.PickCode, node.SHA1, node.Size)
		if node.SourceTreeHash != hash {
			node.SourceTreeHash = hash
			changed = true
		}
		if version != "" && node.TreeVersion != version {
			node.TreeVersion = version
			changed = true
		}
		state[id] = node.IsAlive
		return node.RelativePath, node.IsAlive
	}
	for i := range nodes {
		compute(nodes[i].RemoteFileID)
	}
	return changed
}

func nodesFromTreeItems(lib LibraryConfig, items []TreeItem, version string) []models.P115Node {
	nodes := make([]models.P115Node, 0, len(items))
	for _, item := range items {
		if item.RemoteFileID == "" {
			continue
		}
		nodes = append(nodes, models.P115Node{
			LibraryCID:     lib.CID,
			TreeVersion:    version,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			IsDirectory:    item.IsDirectory,
			IsMedia:        isMediaTreeItem(item),
			IsAlive:        true,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return nodes
}

func treeItemsHaveRemoteIdentity(items []TreeItem) bool {
	for _, item := range items {
		if strings.TrimSpace(item.RemoteFileID) != "" {
			return true
		}
	}
	return false
}

func canMarkMissingFromSource(sourceMode string) bool {
	switch sourceMode {
	case "export":
		return true
	default:
		return false
	}
}

func aliveNodes(nodes []models.P115Node) []models.P115Node {
	out := make([]models.P115Node, 0, len(nodes))
	for _, node := range nodes {
		if node.IsAlive {
			out = append(out, node)
		}
	}
	return out
}

func treeItemsFromNodes(nodes []models.P115Node) []TreeItem {
	items := make([]TreeItem, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsAlive {
			continue
		}
		items = append(items, TreeItem{
			RelativePath:   node.RelativePath,
			Name:           node.Name,
			RemoteFileID:   node.RemoteFileID,
			ParentFileID:   node.ParentFileID,
			PickCode:       node.PickCode,
			SHA1:           node.SHA1,
			Size:           node.Size,
			Depth:          pathDepth(node.RelativePath),
			IsDirectory:    node.IsDirectory,
			SourceTreeHash: node.SourceTreeHash,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].RelativePath < items[j].RelativePath })
	return items
}

func pathDepth(value string) int {
	value = strings.Trim(value, "/")
	if value == "" {
		return 0
	}
	return strings.Count(value, "/") + 1
}

func prepareTreeItems(lib LibraryConfig, items []TreeItem, version string) ([]TreeItem, []models.P115TreeSnapshotItem) {
	prepared := make([]TreeItem, 0, len(items))
	snapshot := make([]models.P115TreeSnapshotItem, 0, len(items))
	for _, item := range items {
		item.SourceTreeHash = sourceHash(lib.CID, item.RelativePath, item.RemoteFileID, item.PickCode, item.SHA1, item.Size)
		media := isMediaTreeItem(item)
		extension := strings.TrimPrefix(strings.ToLower(path.Ext(item.Name)), ".")
		prepared = append(prepared, item)
		snapshot = append(snapshot, models.P115TreeSnapshotItem{
			LibraryCID:     lib.CID,
			TreeVersion:    version,
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			Extension:      extension,
			Depth:          item.Depth,
			IsDirectory:    item.IsDirectory,
			IsMedia:        media,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return prepared, snapshot
}

func treeItemsFromSnapshot(snapshot []models.P115TreeSnapshotItem) []TreeItem {
	items := make([]TreeItem, 0, len(snapshot))
	for _, item := range snapshot {
		isDirectory := item.IsDirectory || (!item.IsMedia && item.Extension == "")
		items = append(items, TreeItem{
			RelativePath:   item.RelativePath,
			Name:           item.Name,
			RemoteFileID:   item.RemoteFileID,
			ParentFileID:   item.ParentFileID,
			PickCode:       item.PickCode,
			SHA1:           item.SHA1,
			Size:           item.Size,
			Depth:          item.Depth,
			IsDirectory:    isDirectory,
			SourceTreeHash: item.SourceTreeHash,
		})
	}
	return items
}

func countMediaTreeItems(items []TreeItem) int {
	count := 0
	for _, item := range items {
		if isMediaTreeItem(item) {
			count++
		}
	}
	return count
}

func exportTreeSummary(items []TreeItem) string {
	return fmt.Sprintf("source=export_dir items=%d media=%d", len(items), countMediaTreeItems(items))
}

func playBaseURL(settings models.P115Settings, fallbackBaseURL string) string {
	if value := strings.TrimSpace(settings.PublicBaseURL); value != "" {
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

func directURLTTL(settings models.P115Settings) time.Duration {
	if settings.DirectURLTTLSeconds <= 0 || settings.DirectURLTTLSeconds == legacyDirectURLTTLSeconds {
		return defaultDirectURLTTL
	}
	return time.Duration(settings.DirectURLTTLSeconds) * time.Second
}

func logDirectResolve(link models.STRMLink, requestUA, effectiveUA, source string, shared bool, directURL string, elapsed time.Duration, errText string) {
	targetHost := ""
	if parsed, err := url.Parse(directURL); err == nil {
		targetHost = parsed.Host
	}
	fields := fmt.Sprintf("link=%s source=%s shared=%t request_ua=%q effective_ua=%q target_host=%q elapsed_ms=%d",
		shortLogValue(link.ID, 16), source, shared, shortLogValue(requestUA, 120), shortLogValue(effectiveUA, 120), targetHost, elapsed.Milliseconds())
	if errText != "" {
		playdiag.Printf("curio play p115 resolve failed %s err=%s", fields, errText)
		return
	}
	playdiag.Printf("curio play p115 resolve ok %s", fields)
}

func shortLogValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
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

func writeSTRM(root, target, playPath string) (bool, error) {
	if strings.TrimSpace(playPath) == "" {
		return false, errors.New("STRM 播放地址为空")
	}
	if !insideRoot(root, target) {
		return false, errors.New("STRM 路径越界")
	}
	content := []byte(playPath + "\n")
	if current, err := os.ReadFile(target); err == nil {
		if bytes.Equal(current, content) || strings.TrimSpace(string(current)) == strings.TrimSpace(playPath) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, target); err != nil {
		return false, err
	}
	return true, nil
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

func localSTRMFiles(root, outputPrefix string) (map[string]string, error) {
	files := map[string]string{}
	start := root
	for _, part := range splitRelativePath(outputPrefix) {
		start = filepath.Join(start, part)
	}
	if !insideRoot(root, start) {
		return nil, errors.New("STRM 扫描路径越界")
	}
	info, err := os.Stat(start)
	if errors.Is(err, os.ErrNotExist) {
		return files, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return files, nil
	}
	err = filepath.WalkDir(start, func(target string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
			files[cleanPathKey(target)] = target
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func desiredMediaRelativePaths(items []TreeItem) []string {
	paths := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		if !isMediaTreeItem(item) {
			continue
		}
		addUniqueRelativePath(&paths, seen, item.RelativePath)
	}
	return paths
}

func desiredSTRMRelativePaths(items []TreeItem) []string {
	paths := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		if !isMediaTreeItem(item) {
			continue
		}
		rel := normalizedRelativePath(item.RelativePath)
		if rel == "" {
			continue
		}
		ext := path.Ext(rel)
		if ext == "" {
			continue
		}
		addUniqueRelativePath(&paths, seen, strings.TrimSuffix(rel, ext)+".strm")
	}
	return paths
}

func strmLinkRelativePaths(links []models.STRMLink) []string {
	paths := make([]string, 0, len(links))
	seen := map[string]struct{}{}
	for _, link := range links {
		addUniqueRelativePath(&paths, seen, link.RelativePath)
	}
	return paths
}

func localSTRMRelativePaths(root, outputPrefix string, localTargets map[string]string) []string {
	base := root
	for _, part := range splitRelativePath(outputPrefix) {
		base = filepath.Join(base, part)
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(localTargets))
	seen := map[string]struct{}{}
	for _, target := range localTargets {
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(baseAbs, targetAbs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		addUniqueRelativePath(&paths, seen, filepath.ToSlash(rel))
	}
	return paths
}

func detectRelativePrefixShift(existing, desired []string) (string, int, int, bool) {
	desiredSet := map[string]struct{}{}
	for _, rel := range desired {
		if rel = normalizedRelativePath(rel); rel != "" {
			desiredSet[rel] = struct{}{}
		}
	}
	if len(desiredSet) == 0 {
		return "", 0, 0, false
	}
	existingSet := map[string]struct{}{}
	for _, rel := range existing {
		if rel = normalizedRelativePath(rel); rel != "" {
			existingSet[rel] = struct{}{}
		}
	}
	prefixMatches := map[string]int{}
	total := 0
	for rel := range existingSet {
		if _, unchanged := desiredSet[rel]; unchanged {
			continue
		}
		total++
		for desiredRel := range desiredSet {
			prefix, ok := shiftedPrefix(rel, desiredRel)
			if ok {
				prefixMatches[prefix]++
			}
		}
	}
	if total == 0 {
		return "", 0, 0, false
	}
	bestPrefix := ""
	bestMatched := 0
	for prefix, matched := range prefixMatches {
		if matched > bestMatched || (matched == bestMatched && prefix < bestPrefix) {
			bestPrefix = prefix
			bestMatched = matched
		}
	}
	if bestMatched == 0 {
		return "", 0, total, false
	}
	if total >= 20 && bestMatched*100 >= total*80 {
		return bestPrefix, bestMatched, total, true
	}
	if total >= 3 && bestMatched == total {
		return bestPrefix, bestMatched, total, true
	}
	return "", bestMatched, total, false
}

func shiftedPrefix(existingRel, desiredRel string) (string, bool) {
	suffix := "/" + existingRel
	if !strings.HasSuffix(desiredRel, suffix) {
		return "", false
	}
	prefix := normalizedRelativePath(strings.TrimSuffix(desiredRel, suffix))
	if prefix == "" {
		return "", false
	}
	return prefix, true
}

func addUniqueRelativePath(paths *[]string, seen map[string]struct{}, rel string) {
	rel = normalizedRelativePath(rel)
	if rel == "" {
		return
	}
	if _, ok := seen[rel]; ok {
		return
	}
	seen[rel] = struct{}{}
	*paths = append(*paths, rel)
}

func normalizedRelativePath(value string) string {
	parts := splitRelativePath(value)
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

func cleanPathKey(target string) string {
	if abs, err := filepath.Abs(target); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(target)
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

func linkIDFromPlayRoute(route string) string {
	route = strings.Trim(strings.ReplaceAll(route, "\\", "/"), "/")
	if !strings.HasPrefix(route, "id/") {
		return ""
	}
	rest := strings.TrimPrefix(route, "id/")
	if cut := strings.IndexByte(rest, '/'); cut >= 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
}

func playRouteFileName(linkID, displayName string) string {
	name := cleanPlayDisplayName(displayName)
	ext := strings.ToLower(path.Ext(name))
	if ext != "" && mediaExtension(ext) {
		return linkID + ext
	}
	return linkID
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

func playPathCandidatesForBases(route string, bases ...string) []string {
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, base := range bases {
		for _, value := range playPathCandidates(base, route) {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
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
