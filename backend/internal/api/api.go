package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"curio/internal/classifier"
	"curio/internal/clouddrive"
	"curio/internal/curated"
	"curio/internal/embyproxy"
	"curio/internal/models"
	"curio/internal/naming"
	"curio/internal/netproxy"
	"curio/internal/p115"
	"curio/internal/playdiag"
	"curio/internal/repository"
	"curio/internal/scraper"
	"curio/internal/worker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
)

type API struct {
	store      *repository.Store
	worker     *worker.Service
	scraper    *scraper.Client
	redis      *redis.Client
	p115       *p115.Service
	curated    *curated.Service
	adminToken string
}

const adminCookieName = "curio_admin_token"

type rearchivePayload struct {
	TMDBID        int    `json:"tmdb_id"`
	MediaType     string `json:"media_type"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	SeasonOffset  int    `json:"season_offset"`
	EpisodeOffset int    `json:"episode_offset"`
	SourcePath    string `json:"source_path"`
	TargetRoot    string `json:"target_root"`
}

func (p rearchivePayload) options() worker.RearchiveOptions {
	return worker.RearchiveOptions{
		TMDBID:        p.TMDBID,
		MediaType:     p.MediaType,
		Season:        p.Season,
		Episode:       p.Episode,
		SeasonOffset:  p.SeasonOffset,
		EpisodeOffset: p.EpisodeOffset,
		SourcePath:    p.SourcePath,
		TargetRoot:    p.TargetRoot,
	}
}

func New(store *repository.Store, workerService *worker.Service, scraperClient *scraper.Client, redisClient *redis.Client, allowedOrigin, frontendDir string) http.Handler {
	p115Service := p115.NewService(store)
	return NewWithP115(store, workerService, scraperClient, redisClient, p115Service, allowedOrigin, frontendDir, "")
}

func NewWithP115(store *repository.Store, workerService *worker.Service, scraperClient *scraper.Client, redisClient *redis.Client, p115Service *p115.Service, allowedOrigin, frontendDir, adminToken string) http.Handler {
	return NewWithServices(store, workerService, scraperClient, redisClient, p115Service, nil, allowedOrigin, frontendDir, adminToken)
}

func NewWithServices(store *repository.Store, workerService *worker.Service, scraperClient *scraper.Client, redisClient *redis.Client, p115Service *p115.Service, curatedService *curated.Service, allowedOrigin, frontendDir, adminToken string) http.Handler {
	api := &API{store: store, worker: workerService, scraper: scraperClient, redis: redisClient, p115: p115Service, curated: curatedService, adminToken: strings.TrimSpace(adminToken)}
	router := chi.NewRouter()
	router.Use(recoverJSON)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Curio-Token"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	router.Use(api.authMiddleware)
	router.Get("/api/auth/status", api.authStatus)
	router.Post("/api/auth/login", api.authLogin)
	router.Get("/api/health", api.health)
	router.Get("/api/events", api.events)
	router.Post("/api/scan/start", api.startScan)
	router.Post("/api/scan/clouddrive/start", api.startCloudDriveScan)
	router.Get("/api/tasks/active", api.activeTask)
	router.Post("/api/tasks/{batchID}/stop", api.stopTask)
	router.Get("/api/batches", api.batches)
	router.Get("/api/batches/{batchID}", api.batch)
	router.Get("/api/stats", api.stats)
	router.Get("/api/settings/directories", api.directories)
	router.Put("/api/settings/directories", api.saveDirectories)
	router.Post("/api/settings/secrets/reveal", api.revealSettingSecret)
	router.Get("/api/settings/system", api.systemSettings)
	router.Put("/api/settings/system", api.saveSystemSettings)
	router.Get("/api/settings/clouddrive", api.cloudDriveSettings)
	router.Put("/api/settings/clouddrive", api.saveCloudDriveSettings)
	router.Get("/api/settings/p115", api.p115Settings)
	router.Put("/api/settings/p115", api.saveP115Settings)
	router.Post("/api/p115/auth/qrcode/start", api.startP115QRCode)
	router.Get("/api/p115/auth/qrcode/{uid}/status", api.p115QRCodeStatus)
	router.Post("/api/p115/auth/qrcode/complete", api.completeP115QRCode)
	router.Post("/api/p115/auth/oauth/start", api.startP115OAuth)
	router.Get("/api/p115/auth/oauth/callback", api.p115OAuthCallback)
	router.Post("/api/p115/auth/import-token", api.importP115Token)
	router.Post("/api/p115/auth/refresh", api.refreshP115Token)
	router.Post("/api/p115/test", api.testP115)
	router.Post("/api/p115/export-tree", api.exportP115Tree)
	router.Post("/api/p115/nodes/rebuild", api.rebuildP115Nodes)
	router.Post("/api/p115/strm/preview", api.previewP115STRM)
	router.Post("/api/p115/strm/sync", api.syncP115STRM)
	router.Post("/api/p115/strm/cleanup", api.cleanupP115STRM)
	router.Get("/api/p115/sync-runs", api.p115SyncRuns)
	router.Get("/api/p115/playback/logs", api.p115PlaybackLogs)
	router.Get("/api/logs", api.logs)
	router.Get("/api/logs/{logID}", api.logDetail)
	router.Get("/api/settings/classification", api.classification)
	router.Put("/api/settings/classification", api.saveClassification)
	router.Get("/api/settings/templates", api.templates)
	router.Put("/api/settings/templates/{templateType}", api.saveTemplate)
	router.Post("/api/settings/templates/preview", api.previewTemplate)
	router.Get("/api/media-files", api.mediaFiles)
	router.Post("/api/media-files/bulk-delete", api.bulkDeleteMediaFiles)
	router.Post("/api/media-files/bulk-rearchive", api.bulkRearchiveMediaFiles)
	router.Delete("/api/media-files/{fileID}", api.deleteMediaFile)
	router.Post("/api/media-files/{fileID}/rearchive", api.rearchiveMediaFile)
	router.Post("/api/clouddrive/test", api.testCloudDrive)
	router.Get("/api/clouddrive/files", api.cloudDriveFiles)
	router.Get("/api/staging", api.staging)
	router.Get("/api/failed", api.failed)
	router.Get("/api/tv-shows", api.tvShows)
	router.Get("/api/tv-shows/{showID}", api.tvShow)
	router.Get("/api/collections", api.collections)
	router.Post("/api/collections/repair-complete", api.repairCompleteCollections)
	router.Get("/api/collections/{collectionID}", api.collection)
	router.Get("/api/curated-collections/{listID}", api.curatedCollection)
	router.Post("/api/curated-collections/{listID}/refresh", api.refreshCuratedCollection)
	router.Get("/play/115/*", api.play115)
	router.Head("/play/115/*", api.play115)
	router.Mount("/emby", embyproxy.New(store, p115Service))
	if strings.TrimSpace(frontendDir) != "" {
		router.Handle("/*", spa(frontendDir))
	}
	return router
}

func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.adminToken == "" || !strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		if !a.validAdminToken(a.authTokenFromRequest(r)) {
			writeError(w, http.StatusUnauthorized, "需要 Curio 管理令牌")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) {
	token := a.authTokenFromRequest(r)
	authenticated := a.adminToken == "" || a.validAdminToken(token)
	if a.adminToken != "" && authenticated {
		setAdminCookie(w, r, token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": a.adminToken != "", "authenticated": authenticated})
}

func (a *API) authLogin(w http.ResponseWriter, r *http.Request) {
	if a.adminToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": false})
		return
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(payload.Token)), []byte(a.adminToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "管理令牌无效")
		return
	}
	setAdminCookie(w, r, payload.Token)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": true})
}

func (a *API) authTokenFromRequest(r *http.Request) string {
	token := strings.TrimSpace(r.Header.Get("X-Curio-Token"))
	if token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	if cookie, err := r.Cookie(adminCookieName); err == nil {
		return decodeCookieToken(cookie.Value)
	}
	if r.URL.Path == "/api/events" {
		return strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return ""
}

func (a *API) validAdminToken(token string) bool {
	token = strings.TrimSpace(token)
	if a.adminToken == "" {
		return true
	}
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.adminToken)) == 1
}

func setAdminCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    encodeCookieToken(strings.TrimSpace(token)),
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
}

func encodeCookieToken(token string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

func decodeCookieToken(value string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(decoded))
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前连接不支持实时事件")
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, "data: {}\n\n")
			flusher.Flush()
		}
	}
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	dbStatus := "ok"
	if err := a.store.Ping(ctx); err != nil {
		dbStatus = err.Error()
	}
	redisStatus := "ok"
	if a.redis != nil {
		if err := a.redis.Ping(ctx).Err(); err != nil {
			redisStatus = err.Error()
		}
	}
	queues := map[string]int64{}
	for _, queue := range []string{"queue:scan", "queue:parse", "queue:scrape", "queue:match", "queue:collection_check", "queue:organize", "queue:failed"} {
		if a.redis != nil {
			queues[queue] = a.redis.LLen(ctx, queue).Val()
		}
	}
	active, ok, _ := a.worker.ActiveBatch(ctx)
	var activePayload any
	if ok {
		activePayload = active
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": dbStatus, "redis": redisStatus, "queues": queues, "active_task": activePayload})
}

func (a *API) startScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batchID, err := a.worker.StartScan(ctx)
	if err != nil {
		if errors.Is(err, worker.ErrTaskAlreadyRunning) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"batch_id": batchID, "status": "started"})
}

func (a *API) startCloudDriveScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batchID, err := a.worker.StartCloudDriveScan(ctx)
	if err != nil {
		if errors.Is(err, worker.ErrTaskAlreadyRunning) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, worker.ErrCloudDriveNotReady) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"batch_id": batchID, "status": "started"})
}

func (a *API) activeTask(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, ok, err := a.worker.ActiveBatch(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) stopTask(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, err := a.worker.Stop(ctx, chi.URLParam(r, "batchID"))
	if err != nil {
		if errors.Is(err, worker.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) batch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batch, err := a.store.Batch(ctx, chi.URLParam(r, "batchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (a *API) batches(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	batches, err := a.store.Batches(ctx, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, batches)
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	stats, err := a.store.MediaStats(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) directories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	dirs, err := a.store.Directories(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dirs)
}

func (a *API) saveDirectories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var dirs models.DirectoryConfig
	if err := decodeJSON(w, r, &dirs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalized, err := validateDirectories(dirs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.SaveDirectories(ctx, normalized); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, normalized)
}

func (a *API) revealSettingSecret(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		Scope string `json:"scope"`
		Field string `json:"field"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	scope := strings.TrimSpace(payload.Scope)
	field := strings.TrimSpace(payload.Field)
	value, err := a.settingSecretValue(ctx, scope, field)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"value": value})
}

func (a *API) settingSecretValue(ctx context.Context, scope, field string) (string, error) {
	switch scope {
	case "system":
		settings, err := a.store.Settings(ctx)
		if err != nil {
			return "", err
		}
		switch field {
		case "tmdb_api_key":
			return settings.TMDBAPIKey, nil
		case "network_proxy":
			return settings.NetworkProxy, nil
		case "ai_base_url":
			return settings.AIBaseURL, nil
		case "ai_api_key":
			return settings.AIAPIKey, nil
		case "clouddrive_address":
			return settings.CloudDriveAddress, nil
		case "clouddrive_username":
			return settings.CloudDriveUsername, nil
		case "clouddrive_password":
			return settings.CloudDrivePassword, nil
		case "clouddrive_token":
			return settings.CloudDriveToken, nil
		}
	case "clouddrive":
		settings, err := a.store.CloudDriveSettings(ctx)
		if err != nil {
			return "", err
		}
		switch field {
		case "address":
			return settings.Address, nil
		case "username":
			return settings.Username, nil
		case "password":
			return settings.Password, nil
		case "token":
			return settings.Token, nil
		}
	case "p115":
		settings, err := a.store.P115Settings(ctx)
		if err != nil {
			return "", err
		}
		switch field {
		case "app_secret":
			return settings.AppSecret, nil
		case "cookies":
			return settings.Cookies, nil
		case "public_base_url":
			return settings.PublicBaseURL, nil
		case "emby_upstream_url":
			return settings.EmbyUpstreamURL, nil
		case "emby_api_key":
			return settings.EmbyAPIKey, nil
		}
	}
	return "", errors.New("未支持的敏感配置字段")
}

func (a *API) systemSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicSystemSettings(settings))
}

func (a *API) saveSystemSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.SystemSettings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings.TMDBAPIKey = strings.TrimSpace(settings.TMDBAPIKey)
	settings.NetworkProxy = strings.TrimSpace(settings.NetworkProxy)
	settings.AIBaseURL = strings.TrimRight(strings.TrimSpace(settings.AIBaseURL), "/")
	settings.AIAPIKey = strings.TrimSpace(settings.AIAPIKey)
	settings.AIModel = strings.TrimSpace(settings.AIModel)
	settings.AIFilenamePrompt = strings.TrimSpace(settings.AIFilenamePrompt)
	current, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mergeHiddenSystemSettings(&settings, current)
	settings.ClassificationYAML = current.ClassificationYAML
	if settings.NetworkProxy != "" {
		if _, err := netproxy.Parse(settings.NetworkProxy); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if settings.AIBaseURL != "" {
		parsed, err := url.Parse(settings.AIBaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			writeError(w, http.StatusBadRequest, "AI 接口地址无效")
			return
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			writeError(w, http.StatusBadRequest, "AI 接口地址协议必须是 http 或 https")
			return
		}
	}
	saved, err := a.store.SaveSettings(ctx, settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.scraper.Configure(saved.TMDBAPIKey, saved.NetworkProxy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicSystemSettings(saved))
}

func (a *API) cloudDriveSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicCloudDriveSettings(settings))
}

func (a *API) saveCloudDriveSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.CloudDriveSettings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing, err := a.store.CloudDriveSettings(ctx); err == nil {
		mergeHiddenCloudDriveSettings(&settings, existing)
	}
	normalized, err := validateCloudDriveSettings(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.store.SaveCloudDriveSettings(ctx, normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicCloudDriveSettings(saved))
}

func (a *API) p115Settings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.P115Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	settings.EmbyPublicURL = ""
	if settings.EmbyProxyPort <= 0 {
		settings.EmbyProxyPort = 8097
	}
	settings.CookieLoginApp = p115.NormalizeCookieLoginApp(settings.CookieLoginApp)
	writeJSON(w, http.StatusOK, publicP115Settings(settings))
}

func (a *API) saveP115Settings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.P115Settings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing, err := a.store.P115Settings(ctx); err == nil {
		mergeHiddenP115Settings(&settings, existing)
	}
	normalized, err := validateP115Settings(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := a.store.SaveP115Settings(ctx, normalized)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicP115Settings(saved))
}

func (a *API) startP115QRCode(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	session, err := a.p115.StartQRCode(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (a *API) p115QRCodeStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	status, err := a.p115.QRCodeStatus(ctx, chi.URLParam(r, "uid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) completeP115QRCode(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		UID string `json:"uid"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.p115.CompleteQRCode(ctx, payload.UID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) startP115OAuth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	result, err := a.p115.StartOAuth(ctx, requestBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) p115OAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" {
		writeHTML(w, http.StatusBadRequest, "115 OAuth 登录失败：缺少 code")
		return
	}
	result, err := a.p115.CompleteOAuth(ctx, code, state)
	if err != nil {
		writeHTML(w, http.StatusBadRequest, "115 OAuth 登录失败："+err.Error())
		return
	}
	writeHTML(w, http.StatusOK, result.Message+"，可以关闭此页面")
}

func (a *API) importP115Token(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.p115.ImportOpenToken(ctx, payload.AccessToken, payload.RefreshToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) refreshP115Token(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	result, err := a.p115.RefreshOpenToken(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) testP115(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	status, err := a.p115.Status(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) exportP115Tree(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	result, err := a.p115.ExportTree(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) syncP115STRM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	result, err := a.p115.Sync(ctx, requestBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) previewP115STRM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var settings models.P115Settings
	if err := decodeJSON(w, r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if existing, err := a.store.P115Settings(ctx); err == nil {
		mergeHiddenP115Settings(&settings, existing)
	}
	normalized, err := validateP115Settings(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	preview, err := a.p115.PreviewSTRM(ctx, normalized, requestBaseURL(r), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicSTRMPreview(preview))
}

func (a *API) rebuildP115Nodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	result, err := a.p115.RebuildNodes(ctx, requestBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) cleanupP115STRM(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := a.p115.Cleanup(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) p115SyncRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := a.p115.SyncRuns(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (a *API) p115PlaybackLogs(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginationFromRequest(r)
	writeJSON(w, http.StatusOK, publicLogPage(playbackLogPage("playback", limit, offset)))
}

func (a *API) logs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	logType := strings.TrimSpace(r.URL.Query().Get("type"))
	if logType == "" {
		logType = "all"
	}
	if logType == "playback" {
		writeJSON(w, http.StatusOK, publicLogPage(playbackLogPage(logType, limit, offset)))
		return
	}
	dbLimit, dbOffset := limit, offset
	playbackPage := models.LogPage{}
	if logType == "all" {
		playbackPage = playbackLogPage("playback", len(playdiag.Records(0)), 0)
		dbOffset = offset - playbackPage.Total
		if dbOffset < 0 {
			dbOffset = 0
		}
		dbLimit = limit + playbackPage.Total
	}
	page, err := a.store.LogEntries(ctx, logType, dbLimit, dbOffset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logType == "all" {
		merged := append([]models.LogEntry{}, page.Items...)
		merged = append(merged, playbackPage.Items...)
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].CreatedAt.After(merged[j].CreatedAt)
		})
		total := page.Total + playbackPage.Total
		start := offset - dbOffset
		if start < 0 {
			start = 0
		}
		if start > len(merged) {
			merged = nil
		} else {
			end := start + limit
			if end > len(merged) {
				end = len(merged)
			}
			merged = merged[start:end]
		}
		page = models.LogPage{Items: merged, Total: total, Limit: limit, Offset: offset, Type: logType}
	} else {
		page.Limit = limit
		page.Offset = offset
	}
	writeJSON(w, http.StatusOK, publicLogPage(page))
}

func (a *API) logDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	entry, err := a.store.LogEntry(ctx, chi.URLParam(r, "logID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "日志不存在")
		return
	}
	writeJSON(w, http.StatusOK, publicLogEntry(entry))
}

func playbackLogPage(logType string, limit, offset int) models.LogPage {
	records := playdiag.Records(0)
	items := make([]models.LogEntry, 0, len(records))
	for index, record := range records {
		status := "ok"
		lower := strings.ToLower(record.Message)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "err=") || strings.Contains(lower, "error") {
			status = "failed"
		}
		items = append(items, models.LogEntry{
			ID:        fmt.Sprintf("playback-%d-%d", record.Time.UnixNano(), index),
			Type:      "playback",
			Source:    "播放诊断",
			Status:    status,
			Message:   record.Message,
			Detail:    record.Message,
			CreatedAt: record.Time,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return models.LogPage{Items: items[offset:end], Total: total, Limit: limit, Offset: offset, Type: logType}
}

func (a *API) play115(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	var directURL string
	var err error
	if token != "" {
		if _, err := a.p115.LinkIDFromToken(token); err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		directURL, err = a.p115.ResolvePlayURL(ctx, token, r.UserAgent())
	} else {
		route := strings.TrimSpace(chi.URLParam(r, "*"))
		directURL, err = a.p115.ResolvePlayURLFromRoute(ctx, route, requestBaseURL(r), r.UserAgent())
	}
	if err != nil {
		logPlay115(r, "", err.Error())
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	logPlay115(r, directURL, "")
	writePlayRedirect(w, directURL)
}

func writePlayRedirect(w http.ResponseWriter, directURL string) {
	h := w.Header()
	h.Set("Cache-Control", "no-store, no-cache, must-revalidate")
	h.Set("Pragma", "no-cache")
	h.Set("Expires", "0")
	h.Set("Vary", "User-Agent")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", "Location")
	h.Set("Location", directURL)
	h.Set("X-Curio-Redirect", "115")
	w.WriteHeader(http.StatusFound)
}

func logPlay115(r *http.Request, directURL, errText string) {
	if r == nil {
		return
	}
	targetHost := ""
	if parsed, err := url.Parse(directURL); err == nil {
		targetHost = parsed.Host
	}
	if errText != "" {
		playdiag.Printf("curio play api failed method=%s path=%q ua=%q err=%s", r.Method, r.URL.RequestURI(), r.UserAgent(), errText)
		return
	}
	playdiag.Printf("curio play api redirect method=%s path=%q ua=%q target_host=%q", r.Method, r.URL.RequestURI(), r.UserAgent(), targetHost)
}

func (a *API) classification(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.Settings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"classification_yaml": settings.ClassificationYAML})
}

func (a *API) saveClassification(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		ClassificationYAML string `json:"classification_yaml"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := classifier.Parse(payload.ClassificationYAML); err != nil {
		writeError(w, http.StatusBadRequest, "分类 YAML 无效："+err.Error())
		return
	}
	saved, err := a.store.SaveClassification(ctx, payload.ClassificationYAML)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"classification_yaml": saved.ClassificationYAML})
}

func (a *API) templates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	templates, err := a.store.Templates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (a *API) saveTemplate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	templateType := chi.URLParam(r, "templateType")
	var payload models.NamingTemplate
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := naming.Validate(templateType, payload.Template); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(payload.Name) == "" {
		payload.Name = templateType
	}
	payload.TemplateType = templateType
	payload.Enabled = true
	if err := a.store.SaveTemplate(ctx, payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) previewTemplate(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		TemplateType string `json:"template_type"`
		Template     string `json:"template"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := naming.Preview(payload.TemplateType, payload.Template)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"preview": preview})
}

func (a *API) mediaFiles(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, "")
}

func (a *API) staging(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, models.StatusDone)
}

func (a *API) failed(w http.ResponseWriter, r *http.Request) {
	a.mediaByStatus(w, r, models.StatusFailed)
}

func (a *API) mediaByStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	if status == "" {
		status = strings.TrimSpace(r.URL.Query().Get("status"))
	}
	files, err := a.store.ListMediaFiles(ctx, status, strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func paginationFromRequest(r *http.Request) (int, int) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 200 {
			limit = value
		}
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			offset = value
		}
	}
	return limit, offset
}

func (a *API) bulkDeleteMediaFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	var payload struct {
		FileIDs []string `json:"file_ids"`
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := cleanIDs(payload.FileIDs)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	batchIDs, err := a.store.DeleteMediaFiles(ctx, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, batchID := range batchIDs {
		_ = a.store.RecountBatch(ctx, batchID)
	}
	_ = a.store.RefreshCollectionLocalCounts(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": len(ids)})
}

func (a *API) deleteMediaFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	fileID := strings.TrimSpace(chi.URLParam(r, "fileID"))
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	file, err := a.store.MediaFile(ctx, fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := a.store.DeleteMediaFile(ctx, fileID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.RecountBatch(ctx, file.BatchID)
	_ = a.store.RefreshCollectionLocalCounts(ctx)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) rearchiveMediaFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var payload rearchivePayload
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, err := a.worker.RearchiveMedia(ctx, chi.URLParam(r, "fileID"), payload.options())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (a *API) bulkRearchiveMediaFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()
	var payload struct {
		FileIDs []string `json:"file_ids"`
		rearchivePayload
	}
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ids := cleanIDs(payload.FileIDs)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "媒体文件 ID 不能为空")
		return
	}
	files, failures, err := a.worker.RearchiveMediaBatch(ctx, ids, payload.options())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": files, "count": len(files), "failed": len(failures), "errors": failures})
}

func (a *API) testCloudDrive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, err := clouddrive.New(settings).Test(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) cloudDriveFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	settings, err := a.store.CloudDriveSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir := r.URL.Query().Get("path")
	if strings.TrimSpace(dir) == "" {
		dir = settings.RootPath
	}
	files, err := clouddrive.New(settings).List(ctx, dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (a *API) collections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	collections, err := a.store.Collections(ctx, strings.TrimSpace(r.URL.Query().Get("q")), strings.TrimSpace(r.URL.Query().Get("status")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collections)
}

func (a *API) repairCompleteCollections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	count, err := a.worker.RepairCompleteCollections(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "count": count})
}

func (a *API) tvShows(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	limit, offset := paginationFromRequest(r)
	shows, err := a.store.TVShows(ctx, strings.TrimSpace(r.URL.Query().Get("q")), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, shows)
}

func (a *API) tvShow(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	id, err := strconv.Atoi(chi.URLParam(r, "showID"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "剧集 ID 无效")
		return
	}
	show, err := a.store.TVShow(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if needsTVShowRefresh(show) {
		if refreshed, episodes, refreshErr := a.scraper.RefreshTVShow(ctx, id); refreshErr == nil {
			if saveErr := a.store.UpsertTVShow(ctx, refreshed); saveErr == nil {
				episodeIDs := make([]string, 0, len(episodes))
				for _, episode := range episodes {
					if err := a.store.UpsertTVEpisode(ctx, episode); err != nil {
						saveErr = err
						break
					}
					episodeIDs = append(episodeIDs, episode.ID)
				}
				if saveErr == nil && len(episodes) >= refreshed.EpisodeCount {
					_ = a.store.DeleteTVEpisodesNotIn(ctx, refreshed.TMDBID, episodeIDs)
				}
			}
			if next, err := a.store.TVShow(ctx, id); err == nil {
				show = next
			}
		}
	}
	writeJSON(w, http.StatusOK, show)
}

func needsTVShowRefresh(show models.TVShowStatus) bool {
	known := show.ReleasedEpisodeCount + show.UnreleasedEpisodeCount
	return (show.EpisodeCount > 0 && known != show.EpisodeCount) || (known == 0 && show.SeasonCount > 0)
}

func (a *API) collection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	id, err := strconv.Atoi(chi.URLParam(r, "collectionID"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "合集 ID 无效")
		return
	}
	collection, err := a.store.Collection(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (a *API) curatedCollection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	id := strings.TrimSpace(chi.URLParam(r, "listID"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "榜单 ID 无效")
		return
	}
	collection, err := a.store.CuratedCollection(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (a *API) refreshCuratedCollection(w http.ResponseWriter, r *http.Request) {
	if a.curated == nil {
		writeError(w, http.StatusBadRequest, "固定榜单刷新服务未启用")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "listID"))
	if id != models.CuratedDoubanTop250ID {
		writeError(w, http.StatusNotFound, "固定榜单不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Minute)
	defer cancel()
	collection, err := a.curated.RefreshTop250(ctx)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func validateDirectories(dirs models.DirectoryConfig) (models.DirectoryConfig, error) {
	values := map[string]string{
		"incoming_path":               dirs.IncomingPath,
		"staging_path":                dirs.StagingPath,
		"failed_path":                 dirs.FailedPath,
		"incomplete_collections_path": dirs.IncompleteCollectionsPath,
	}
	labels := map[string]string{
		"incoming_path":               "入库目录",
		"staging_path":                "整理目录",
		"failed_path":                 "失败目录",
		"incomplete_collections_path": "缺失合集目录",
	}
	normalized := map[string]string{}
	for key, value := range values {
		clean, err := normalizePath(value)
		if err != nil {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": " + err.Error())
		}
		if err := ensureReadWrite(clean); err != nil {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": " + err.Error())
		}
		normalized[key] = clean
	}
	seen := map[string]struct{}{}
	for key, value := range normalized {
		if _, ok := seen[value]; ok {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": 目录路径不能相同")
		}
		seen[value] = struct{}{}
	}
	incoming := normalized["incoming_path"]
	for _, key := range []string{"staging_path", "failed_path", "incomplete_collections_path"} {
		if isInside(incoming, normalized[key]) {
			return models.DirectoryConfig{}, errors.New(labels[key] + ": 目录不能位于入库目录内")
		}
	}
	return models.DirectoryConfig{
		IncomingPath:              normalized["incoming_path"],
		StagingPath:               normalized["staging_path"],
		FailedPath:                normalized["failed_path"],
		IncompleteCollectionsPath: normalized["incomplete_collections_path"],
	}, nil
}

func validateCloudDriveSettings(settings models.CloudDriveSettings) (models.CloudDriveSettings, error) {
	settings.Address = strings.TrimSpace(settings.Address)
	if settings.Address == "" {
		settings.Address = "http://localhost:19798"
	}
	if parsed, err := url.Parse(settings.Address); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return models.CloudDriveSettings{}, errors.New("CloudDrive2 地址协议必须是 http 或 https")
		}
		if parsed.Host == "" {
			return models.CloudDriveSettings{}, errors.New("CloudDrive2 地址缺少主机名")
		}
	}
	settings.Username = strings.TrimSpace(settings.Username)
	settings.Password = strings.TrimSpace(settings.Password)
	settings.Token = strings.TrimSpace(settings.Token)
	settings.RootPath = clouddrive.NormalizePath(settings.RootPath)
	settings.StagingPath = clouddrive.NormalizePath(settings.StagingPath)
	settings.FailedPath = clouddrive.NormalizePath(settings.FailedPath)
	settings.IncompleteCollectionsPath = clouddrive.NormalizePath(settings.IncompleteCollectionsPath)
	if settings.StagingPath == settings.RootPath || settings.FailedPath == settings.RootPath || settings.IncompleteCollectionsPath == settings.RootPath {
		return models.CloudDriveSettings{}, errors.New("CloudDrive2 输出目录不能等于扫描根目录")
	}
	if settings.StagingPath == settings.FailedPath || settings.StagingPath == settings.IncompleteCollectionsPath || settings.FailedPath == settings.IncompleteCollectionsPath {
		return models.CloudDriveSettings{}, errors.New("CloudDrive2 输出目录不能相同")
	}
	return settings, nil
}

const hiddenSecretValue = "********"

func publicSystemSettings(settings models.SystemSettings) models.SystemSettings {
	settings.TMDBAPIKey = redactedValue(settings.TMDBAPIKey)
	settings.NetworkProxy = redactedValue(settings.NetworkProxy)
	settings.AIBaseURL = redactedValue(settings.AIBaseURL)
	settings.AIAPIKey = redactedValue(settings.AIAPIKey)
	settings.CloudDriveAddress = redactedValue(settings.CloudDriveAddress)
	settings.CloudDriveUsername = redactedValue(settings.CloudDriveUsername)
	settings.CloudDrivePassword = redactedValue(settings.CloudDrivePassword)
	settings.CloudDriveToken = redactedValue(settings.CloudDriveToken)
	return settings
}

func publicCloudDriveSettings(settings models.CloudDriveSettings) models.CloudDriveSettings {
	settings.Address = redactedValue(settings.Address)
	settings.Username = redactedValue(settings.Username)
	settings.Password = redactedValue(settings.Password)
	settings.Token = redactedValue(settings.Token)
	return settings
}

func publicP115Settings(settings models.P115Settings) models.P115Settings {
	settings.AppSecret = redactedValue(settings.AppSecret)
	settings.Cookies = redactedValue(settings.Cookies)
	settings.PublicBaseURL = redactedValue(settings.PublicBaseURL)
	settings.EmbyUpstreamURL = redactedValue(settings.EmbyUpstreamURL)
	settings.EmbyPublicURL = ""
	settings.EmbyAPIKey = redactedValue(settings.EmbyAPIKey)
	return settings
}

func publicSTRMPreview(preview models.STRMPreview) models.STRMPreview {
	for i := range preview.Items {
		preview.Items[i].PlayPath = pathOnlyURL(preview.Items[i].PlayPath)
	}
	return preview
}

func publicLogPage(page models.LogPage) models.LogPage {
	for i := range page.Items {
		page.Items[i] = publicLogEntry(page.Items[i])
	}
	return page
}

func publicLogEntry(entry models.LogEntry) models.LogEntry {
	entry.BaseURL = redactedValue(entry.BaseURL)
	entry.ProxyURL = redactedValue(entry.ProxyURL)
	return entry
}

func pathOnlyURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	if parsed.RawQuery != "" {
		return parsed.EscapedPath() + "?" + parsed.RawQuery
	}
	return parsed.EscapedPath()
}

func redactedValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return hiddenSecretValue
}

func isHiddenSecret(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.Trim(value, "*") == ""
}

func mergeHiddenSystemSettings(next *models.SystemSettings, existing models.SystemSettings) {
	if isHiddenSecret(next.TMDBAPIKey) {
		next.TMDBAPIKey = existing.TMDBAPIKey
	}
	if isHiddenSecret(next.NetworkProxy) {
		next.NetworkProxy = existing.NetworkProxy
	}
	if isHiddenSecret(next.AIBaseURL) {
		next.AIBaseURL = existing.AIBaseURL
	}
	if isHiddenSecret(next.AIAPIKey) {
		next.AIAPIKey = existing.AIAPIKey
	}
	if isHiddenSecret(next.CloudDriveAddress) {
		next.CloudDriveAddress = existing.CloudDriveAddress
	}
	if isHiddenSecret(next.CloudDriveUsername) {
		next.CloudDriveUsername = existing.CloudDriveUsername
	}
	if isHiddenSecret(next.CloudDrivePassword) {
		next.CloudDrivePassword = existing.CloudDrivePassword
	}
	if isHiddenSecret(next.CloudDriveToken) {
		next.CloudDriveToken = existing.CloudDriveToken
	}
}

func mergeHiddenCloudDriveSettings(next *models.CloudDriveSettings, existing models.CloudDriveSettings) {
	if isHiddenSecret(next.Address) {
		next.Address = existing.Address
	}
	if isHiddenSecret(next.Username) {
		next.Username = existing.Username
	}
	if isHiddenSecret(next.Password) {
		next.Password = existing.Password
	}
	if isHiddenSecret(next.Token) {
		next.Token = existing.Token
	}
}

func mergeHiddenP115Settings(next *models.P115Settings, existing models.P115Settings) {
	if next.AuthMode == "" {
		next.AuthMode = existing.AuthMode
	}
	if isHiddenSecret(next.AppSecret) {
		next.AppSecret = existing.AppSecret
	}
	if isHiddenSecret(next.Cookies) {
		next.Cookies = existing.Cookies
	}
	if isHiddenSecret(next.PublicBaseURL) {
		next.PublicBaseURL = existing.PublicBaseURL
	}
	if isHiddenSecret(next.EmbyUpstreamURL) {
		next.EmbyUpstreamURL = existing.EmbyUpstreamURL
	}
	if isHiddenSecret(next.EmbyAPIKey) {
		next.EmbyAPIKey = existing.EmbyAPIKey
	}
	if next.AccessToken == "" {
		next.AccessToken = existing.AccessToken
	}
	if next.RefreshToken == "" {
		next.RefreshToken = existing.RefreshToken
	}
	if next.OpenTokenRefreshedAt == nil {
		next.OpenTokenRefreshedAt = existing.OpenTokenRefreshedAt
	}
	if next.DirectURLTTLSeconds == 0 {
		next.DirectURLTTLSeconds = existing.DirectURLTTLSeconds
	}
	if next.CookieLoginApp == "" {
		next.CookieLoginApp = existing.CookieLoginApp
	}
	if next.UserAgentMode == "" {
		next.UserAgentMode = existing.UserAgentMode
	}
	if next.FixedUserAgent == "" {
		next.FixedUserAgent = existing.FixedUserAgent
	}
	if next.KeepDeletedDays == 0 {
		next.KeepDeletedDays = existing.KeepDeletedDays
	}
}

func validateP115Settings(settings models.P115Settings) (models.P115Settings, error) {
	settings.Enabled = true
	settings.AppID = strings.TrimSpace(settings.AppID)
	settings.AppSecret = strings.TrimSpace(settings.AppSecret)
	settings.AccessToken = strings.TrimSpace(settings.AccessToken)
	settings.RefreshToken = strings.TrimSpace(settings.RefreshToken)
	settings.Cookies = strings.TrimSpace(settings.Cookies)
	settings.CookieLoginApp = p115.NormalizeCookieLoginApp(settings.CookieLoginApp)
	settings.AuthMode = strings.ToLower(strings.TrimSpace(settings.AuthMode))
	if settings.AuthMode == "" {
		if settings.Cookies != "" {
			settings.AuthMode = "cookies"
		} else if settings.AccessToken != "" || settings.RefreshToken != "" {
			settings.AuthMode = "open"
		} else {
			settings.AuthMode = "open"
		}
	}
	if settings.AuthMode == "cookies" && settings.Cookies == "" && (settings.AccessToken != "" || settings.RefreshToken != "") {
		settings.AuthMode = "open"
	}
	if settings.AuthMode != "cookies" && settings.AuthMode != "open" {
		return models.P115Settings{}, errors.New("115 授权方式必须是 cookies 或 open")
	}
	settings.STRMOutputPath = p115.NormalizeSTRMOutputPaths(settings.STRMOutputPath)
	if settings.STRMOutputPath == "" {
		settings.STRMOutputPath = "/data/Curio/strm"
	}
	settings.PublicBaseURL = strings.TrimRight(strings.TrimSpace(settings.PublicBaseURL), "/")
	settings.EmbyPublicURL = ""
	for label, raw := range map[string]string{"STRM 生成地址": settings.PublicBaseURL} {
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return models.P115Settings{}, errors.New(label + "无效")
		}
	}
	settings.DirectURLTTLSeconds = 3000
	settings.UserAgentMode = "inherit"
	settings.FixedUserAgent = ""
	settings.LibraryCID = strings.TrimSpace(settings.LibraryCID)
	if settings.LibraryCID != "" {
		libs, err := p115.ParseLibraryCIDs(settings.LibraryCID)
		if err != nil {
			return models.P115Settings{}, err
		}
		libs, err = p115.ApplyLibraryOutputRoots(libs, settings.STRMOutputPath)
		if err != nil {
			return models.P115Settings{}, err
		}
		settings.LibraryCID = p115.FormatLibraryCIDs(libs)
		settings.STRMOutputPath = p115.FormatLibraryOutputRoots(libs)
	}
	if settings.KeepDeletedDays <= 0 {
		settings.KeepDeletedDays = 7
	}
	if settings.SyncIntervalMinutes <= 0 {
		settings.SyncIntervalMinutes = 60
	}
	if settings.SyncIntervalMinutes < 5 || settings.SyncIntervalMinutes > 10080 {
		return models.P115Settings{}, errors.New("115 定时同步间隔必须在 5 到 10080 分钟之间")
	}
	settings.EmbyUpstreamURL = strings.TrimRight(strings.TrimSpace(settings.EmbyUpstreamURL), "/")
	if settings.EmbyUpstreamURL != "" {
		parsed, err := url.Parse(settings.EmbyUpstreamURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return models.P115Settings{}, errors.New("Emby 原始地址无效")
		}
	}
	if settings.EmbyProxyPort == 0 {
		settings.EmbyProxyPort = 8097
	}
	if settings.EmbyProxyPort < 1 || settings.EmbyProxyPort > 65535 {
		return models.P115Settings{}, errors.New("Emby 反代端口无效")
	}
	settings.EmbyProxyBasePath = "/" + strings.Trim(strings.TrimSpace(settings.EmbyProxyBasePath), "/")
	if settings.EmbyProxyBasePath == "/" {
		settings.EmbyProxyBasePath = "/emby"
	}
	settings.EmbyAPIKey = strings.TrimSpace(settings.EmbyAPIKey)
	return settings, nil
}

func clampInt(value, min, max, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func cleanIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("路径不能为空")
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", errors.New("路径包含非法跳转")
		}
	}
	clean := filepath.Clean(value)
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return "", err
		}
		clean = abs
	}
	return clean, nil
}

func ensureReadWrite(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(path, ".curio-rw-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

func isInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func contextWithTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 10*time.Second)
}

func recoverJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("api panic: %v\n%s", recovered, debug.Stack())
				writeError(w, http.StatusInternalServerError, "服务处理请求时异常退出，请查看后端日志")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestBaseURL(r *http.Request) string {
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

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeHTML(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	escaped := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(message)
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>Curio</title><body style=\"font-family:system-ui,sans-serif;padding:32px;color:#202124\">%s</body>", escaped)
}

func spa(dir string) http.HandlerFunc {
	files := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join(dir, cleanPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	}
}
