package p115

import (
	"errors"
	"path"
	"strings"

	"curio/internal/models"

	"gopkg.in/yaml.v3"
)

const (
	authModeCookies = "cookies"
	authModeOpen    = "open"

	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124 Safari/537.36"
)

var cookieLoginApps = map[string]string{
	"web":        "web",
	"desktop":    "web",
	"android":    "android",
	"115android": "115android",
	"ios":        "ios",
	"115ios":     "115ios",
	"ipad":       "ipad",
	"115ipad":    "115ipad",
	"tv":         "tv",
	"apple_tv":   "apple_tv",
	"qandroid":   "qandroid",
	"qandriod":   "qandroid",
	"qios":       "qios",
	"qipad":      "qipad",
	"wechatmini": "wechatmini",
	"alipaymini": "alipaymini",
	"harmony":    "harmony",
}

type LibraryConfig struct {
	Name         string `json:"name" yaml:"name"`
	CID          string `json:"cid" yaml:"cid"`
	Type         string `json:"type" yaml:"type"`
	OutputPrefix string `json:"output_prefix" yaml:"output_prefix"`
	LayerLimit   int    `json:"layer_limit" yaml:"layer_limit"`
}

type SyncConfig struct {
	DeleteMissingSTRM    bool `json:"delete_missing_strm" yaml:"delete_missing_strm"`
	StaleBeforeDelete    bool `json:"stale_before_delete" yaml:"stale_before_delete"`
	KeepDeletedDays      int  `json:"keep_deleted_days" yaml:"keep_deleted_days"`
	RefreshEmbyAfterSync bool `json:"refresh_emby_after_sync" yaml:"refresh_emby_after_sync"`
}

type LibrariesConfig struct {
	Libraries []LibraryConfig `json:"libraries" yaml:"libraries"`
	Sync      SyncConfig      `json:"sync" yaml:"sync"`
}

func ParseLibraries(value string) (LibrariesConfig, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return LibrariesConfig{}, errors.New("115 媒体库 CID 配置不能为空")
	}
	if isPlainCID(value) {
		return normalizeLibraries(LibrariesConfig{
			Libraries: []LibraryConfig{{
				Name:       "媒体库",
				CID:        value,
				Type:       "root",
				LayerLimit: 25,
			}},
			Sync: SyncConfig{
				DeleteMissingSTRM:    true,
				RefreshEmbyAfterSync: false,
			},
		})
	}
	var cfg LibrariesConfig
	if err := yaml.Unmarshal([]byte(value), &cfg); err != nil {
		return LibrariesConfig{}, err
	}
	return normalizeLibraries(cfg)
}

func normalizeLibraries(cfg LibrariesConfig) (LibrariesConfig, error) {
	seen := map[string]struct{}{}
	for i := range cfg.Libraries {
		lib := &cfg.Libraries[i]
		lib.CID = strings.TrimSpace(lib.CID)
		lib.Name = strings.TrimSpace(lib.Name)
		lib.Type = strings.TrimSpace(lib.Type)
		lib.OutputPrefix = strings.Trim(strings.TrimSpace(lib.OutputPrefix), "/\\")
		if lib.CID == "" {
			return LibrariesConfig{}, errors.New("115 媒体库 cid 不能为空")
		}
		if _, ok := seen[lib.CID]; ok {
			return LibrariesConfig{}, errors.New("115 媒体库 cid 不能重复：" + lib.CID)
		}
		seen[lib.CID] = struct{}{}
		if lib.Name == "" {
			lib.Name = lib.CID
		}
		if lib.Type == "" {
			lib.Type = "media"
		}
		if lib.OutputPrefix == "" {
			lib.OutputPrefix = outputPrefix(lib)
		}
		if lib.LayerLimit <= 0 {
			lib.LayerLimit = 25
		}
		if strings.Contains(lib.OutputPrefix, "..") {
			return LibrariesConfig{}, errors.New("115 STRM 输出前缀不能包含路径跳转")
		}
	}
	if len(cfg.Libraries) == 0 {
		return LibrariesConfig{}, errors.New("至少需要配置一个 115 媒体库 cid")
	}
	if cfg.Sync.KeepDeletedDays == 0 {
		cfg.Sync.KeepDeletedDays = 7
	}
	return cfg, nil
}

func libraryCIDs(cfg LibrariesConfig) map[string]struct{} {
	out := make(map[string]struct{}, len(cfg.Libraries))
	for _, lib := range cfg.Libraries {
		out[lib.CID] = struct{}{}
	}
	return out
}

func outputPrefix(lib *LibraryConfig) string {
	switch strings.ToLower(lib.Type) {
	case "root", "library":
		return ""
	case "movie", "movies":
		return "movies"
	case "tv", "show", "series":
		return "tv"
	case "collection", "collections":
		return "collections"
	default:
		if lib.Name != "" {
			return strings.Trim(path.Clean(lib.Name), "/")
		}
		return "media"
	}
}

func isPlainCID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n:") {
		return false
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") || strings.Contains(value, " ") || strings.Contains(value, "\t") {
		return false
	}
	return true
}

func NormalizeCookieLoginApp(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := cookieLoginApps[value]; ok {
		return normalized
	}
	return "wechatmini"
}

func userAgent(_ models.P115Settings, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return defaultUserAgent
}

func mediaExtension(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "mkv", "mp4", "m4v", "mov", "avi", "wmv", "ts", "m2ts", "mts", "iso", "flv", "webm", "mpg", "mpeg":
		return true
	default:
		return false
	}
}
