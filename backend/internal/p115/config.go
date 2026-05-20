package p115

import (
	"errors"
	"fmt"
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
	OutputRoot   string `json:"output_root"`
	OutputPrefix string `json:"output_prefix"`
	LayerLimit   int    `json:"layer_limit"`
}

func ParseLibraryCID(value string) (LibraryConfig, error) {
	cid := strings.TrimSpace(value)
	if cid == "" {
		return LibraryConfig{}, errors.New("115 媒体库 CID 不能为空")
	}
	if !validLibraryCID(cid) {
		return LibraryConfig{}, errors.New("115 媒体库 CID 只能填写单个目录 CID")
	}
	return LibraryConfig{
		Name:       "媒体库",
		CID:        cid,
		Type:       "root",
		LayerLimit: 25,
	}, nil
}

func ParseLibraryCIDs(value string) ([]LibraryConfig, error) {
	parts := splitLibraryCIDList(value)
	if len(parts) == 0 {
		return nil, errors.New("115 媒体库 CID 不能为空")
	}
	libs := make([]LibraryConfig, 0, len(parts))
	seen := map[string]struct{}{}
	for _, cid := range parts {
		if !validLibraryCID(cid) {
			return nil, fmt.Errorf("115 媒体库 CID 无效：%s", cid)
		}
		if _, ok := seen[cid]; ok {
			continue
		}
		seen[cid] = struct{}{}
		libs = append(libs, LibraryConfig{
			Name:       "媒体库",
			CID:        cid,
			Type:       "root",
			LayerLimit: 25,
		})
	}
	if len(libs) > 1 {
		for index := range libs {
			libs[index].Name = fmt.Sprintf("媒体库 %d", index+1)
		}
	}
	return libs, nil
}

func FormatLibraryCIDs(libs []LibraryConfig) string {
	cids := make([]string, 0, len(libs))
	for _, lib := range libs {
		if cid := strings.TrimSpace(lib.CID); cid != "" {
			cids = append(cids, cid)
		}
	}
	return strings.Join(cids, "\n")
}

func SplitSTRMOutputPaths(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\r' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if path := strings.TrimSpace(part); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func NormalizeSTRMOutputPaths(value string) string {
	paths := SplitSTRMOutputPaths(value)
	if len(paths) == 0 {
		return ""
	}
	return strings.Join(paths, "\n")
}

func ApplyLibraryOutputRoots(libs []LibraryConfig, value string) ([]LibraryConfig, error) {
	paths := SplitSTRMOutputPaths(value)
	if len(paths) == 0 {
		return nil, errors.New("STRM 输出目录不能为空")
	}
	if len(paths) > 1 && len(paths) != len(libs) {
		return nil, fmt.Errorf("STRM 输出目录数量必须为 1 个或与媒体库 CID 数量一致（当前 CID %d 个，输出目录 %d 个）", len(libs), len(paths))
	}
	out := append([]LibraryConfig(nil), libs...)
	for index := range out {
		if len(paths) == 1 {
			out[index].OutputRoot = paths[0]
		} else {
			out[index].OutputRoot = paths[index]
		}
	}
	return out, nil
}

func FormatLibraryOutputRoots(libs []LibraryConfig) string {
	roots := make([]string, 0, len(libs))
	seenDifferent := false
	for _, lib := range libs {
		root := strings.TrimSpace(lib.OutputRoot)
		if root == "" {
			continue
		}
		if len(roots) > 0 && root != roots[0] {
			seenDifferent = true
		}
		roots = append(roots, root)
	}
	if len(roots) == 0 {
		return ""
	}
	if !seenDifferent {
		return roots[0]
	}
	return strings.Join(roots, "\n")
}

func validLibraryCID(cid string) bool {
	cid = strings.TrimSpace(cid)
	return cid != "" && !strings.ContainsAny(cid, "\r\n: \t")
}

func splitLibraryCIDList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '\r', '\n', ',', '，', ';', '；', ' ', '\t':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if cid := strings.TrimSpace(field); cid != "" {
			out = append(out, cid)
		}
	}
	return out
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
