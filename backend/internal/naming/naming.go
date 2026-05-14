package naming

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"curio/internal/models"
)

var fieldRE = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
var spacesRE = regexp.MustCompile(`\s+`)

var allowedFields = map[string][]string{
	models.TemplateMovie: {
		"title", "year", "category", "resolution", "source", "video_codec", "audio_codec", "audio_channels", "hdr_format", "extension",
	},
	models.TemplateTVEpisode: {
		"show_title", "show_year", "season", "season_2", "episode", "episode_2", "episode_title",
		"category", "resolution", "source", "video_codec", "audio_codec", "audio_channels", "hdr_format", "extension",
	},
	models.TemplateCollectionMovie: {
		"collection_name", "collection_id", "title", "year", "category", "resolution", "source", "video_codec", "audio_codec", "audio_channels", "hdr_format", "extension",
	},
	models.TemplateIncompleteCollection: {
		"collection_name", "collection_id", "title", "year", "category", "resolution", "source", "video_codec", "audio_codec", "audio_channels", "hdr_format", "extension",
	},
}

func DefaultTemplates() []models.NamingTemplate {
	return []models.NamingTemplate{
		{TemplateType: models.TemplateMovie, Name: "电影模板", Template: "movies/{category}/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}", Enabled: true},
		{TemplateType: models.TemplateTVEpisode, Name: "剧集模板", Template: "tv/{category}/{show_title} ({show_year})/Season {season_2}/{show_title} - S{season_2}E{episode_2} - {episode_title} - {resolution} {source} {video_codec}.{extension}", Enabled: true},
		{TemplateType: models.TemplateCollectionMovie, Name: "完整合集模板", Template: "collections/{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}", Enabled: true},
		{TemplateType: models.TemplateIncompleteCollection, Name: "缺失合集模板", Template: "{category}/{collection_name} ({collection_id})/{title} ({year})/{title} ({year}) - {resolution} {source} {video_codec}.{extension}", Enabled: true},
	}
}

func Validate(templateType, template string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return errors.New("模板不能为空")
	}
	if !strings.Contains(template, "{extension}") {
		return errors.New("模板必须包含 {extension}")
	}
	if strings.HasPrefix(template, "/") || strings.HasPrefix(template, `\`) {
		return errors.New("模板必须是相对路径")
	}
	if strings.Contains(template, "..") {
		return errors.New("模板不能包含 ..")
	}
	fields, err := Fields(templateType, template)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return errors.New("模板缺少可用字段")
	}
	for _, part := range splitPath(template) {
		if strings.TrimSpace(part) == "" {
			return errors.New("模板不能生成空路径片段")
		}
	}
	return nil
}

func Fields(templateType, template string) ([]string, error) {
	allowed, ok := allowedFields[templateType]
	if !ok {
		return nil, fmt.Errorf("未知模板类型 %s", templateType)
	}
	matches := fieldRE.FindAllStringSubmatch(template, -1)
	fields := make([]string, 0, len(matches))
	for _, match := range matches {
		field := match[1]
		if !slices.Contains(allowed, field) {
			return nil, fmt.Errorf("字段 %s 不适用于 %s", field, templateType)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func Render(templateType, template string, values map[string]string, root string) (string, string, error) {
	if err := Validate(templateType, template); err != nil {
		return "", "", err
	}
	rendered := fieldRE.ReplaceAllStringFunc(template, func(token string) string {
		key := strings.Trim(token, "{}")
		value := strings.TrimSpace(values[key])
		if value == "" {
			return "未知"
		}
		return value
	})
	segments := splitPath(rendered)
	cleanSegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		clean := CleanSegment(segment)
		if clean == "" {
			return "", "", errors.New("模板生成了空路径片段")
		}
		if utf8.RuneCountInString(clean) > 180 {
			return "", "", errors.New("路径片段超过 180 个字符")
		}
		cleanSegments = append(cleanSegments, clean)
	}
	relative := filepath.Join(cleanSegments...)
	rootClean := filepath.Clean(root)
	target := filepath.Clean(filepath.Join(rootClean, relative))
	if utf8.RuneCountInString(target) > 240 {
		return "", "", errors.New("渲染后的路径超过 240 个字符")
	}
	if !inside(rootClean, target) {
		return "", "", errors.New("渲染后的路径超出根目录")
	}
	return relative, target, nil
}

func Preview(templateType, template string) (string, error) {
	values := map[string]string{
		"title": "盗梦空间", "year": "2010", "category": "剧情", "resolution": "1080p", "source": "BluRay", "video_codec": "AVC", "audio_codec": "TrueHD", "audio_channels": "7.1", "hdr_format": "HDR10", "extension": "mkv",
		"show_title": "最后生还者", "show_year": "2023", "season": "1", "season_2": "01", "episode": "3", "episode_2": "03", "episode_title": "很久很久以前",
		"collection_name": "阿凡达合集", "collection_id": "87096",
	}
	relative, _, err := Render(templateType, template, values, "/preview")
	return relative, err
}

func CollectionRoot(template string, values map[string]string, root string) (string, error) {
	return TemplateRoot(models.TemplateCollectionMovie, template, values, root)
}

func TemplateRoot(templateType, template string, values map[string]string, root string) (string, error) {
	relative, absolute, err := Render(templateType, template, values, root)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(filepath.Dir(relative))
	target := filepath.Clean(filepath.Join(root, dir))
	if !inside(filepath.Clean(root), target) || target == filepath.Dir(absolute) {
		return "", errors.New("无法推导合集根目录")
	}
	return target, nil
}

func CleanSegment(value string) string {
	replacer := strings.NewReplacer(
		"/", "-", `\`, "-", ":", " -", "*", "", "?", "", `"`, "'", "<", "(", ">", ")", "|", "-",
	)
	value = replacer.Replace(value)
	value = spacesRE.ReplaceAllString(value, " ")
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ".")
	if value == "" {
		return "未知"
	}
	return value
}

func splitPath(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
}

func inside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
