package p115

import (
	"errors"
	"strings"

	"curio/internal/models"
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
	Name         string `json:"name"`
	CID          string `json:"cid"`
	Type         string `json:"type"`
	OutputPrefix string `json:"output_prefix"`
	LayerLimit   int    `json:"layer_limit"`
}

func ParseLibraryCID(value string) (LibraryConfig, error) {
	cid := strings.TrimSpace(value)
	if cid == "" {
		return LibraryConfig{}, errors.New("115 媒体库 CID 不能为空")
	}
	if strings.ContainsAny(cid, "\r\n:") || strings.Contains(cid, " ") || strings.Contains(cid, "\t") {
		return LibraryConfig{}, errors.New("115 媒体库 CID 只能填写单个目录 CID")
	}
	return LibraryConfig{
		Name:       "媒体库",
		CID:        cid,
		Type:       "root",
		LayerLimit: 25,
	}, nil
}

func NormalizeCookieLoginApp(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := cookieLoginApps[value]; ok {
		return normalized
	}
	return "wechatmini"
}

func userAgent(settings models.P115Settings, fallback string) string {
	mode := strings.ToLower(strings.TrimSpace(settings.UserAgentMode))
	fixed := strings.TrimSpace(settings.FixedUserAgent)
	switch mode {
	case "fixed":
		if fixed != "" {
			return fixed
		}
	case "default":
		return defaultUserAgent
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	if fixed != "" {
		return fixed
	}
	return defaultUserAgent
}

func DefaultUserAgent() string {
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
