package classifier

import (
	"fmt"
	"strings"

	"curio/internal/models"

	"gopkg.in/yaml.v3"
)

type Item struct {
	GenreIDs            []string
	OriginalLanguage    string
	OriginCountry       []string
	ProductionCountries []string
	Keywords            []string
}

type Config struct {
	Movie []Rule
	TV    []Rule
}

type Rule struct {
	Name      string
	Condition map[string][]string
}

func Parse(raw string) (Config, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Config{}, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return Config{}, err
	}
	if len(root.Content) == 0 {
		return Config{}, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return Config{}, fmt.Errorf("分类配置根节点必须是映射")
	}
	cfg := Config{
		Movie: parseSection(findMapValue(doc, "movie")),
		TV:    parseSection(findMapValue(doc, "tv")),
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Match(raw, mediaType string, item Item) (string, error) {
	cfg, err := Parse(raw)
	if err != nil {
		return "", err
	}
	rules := cfg.Movie
	if mediaType == models.MediaTVEpisode {
		rules = cfg.TV
	}
	for _, rule := range rules {
		matched, err := matchRule(rule, item)
		if err != nil {
			return "", err
		}
		if matched {
			return rule.Name, nil
		}
	}
	return "", nil
}

func parseSection(node *yaml.Node) []Rule {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	rules := make([]Rule, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := strings.TrimSpace(node.Content[i].Value)
		if name == "" {
			continue
		}
		rules = append(rules, Rule{Name: name, Condition: parseCondition(node.Content[i+1])})
	}
	return rules
}

func parseCondition(node *yaml.Node) map[string][]string {
	condition := map[string][]string{}
	if node == nil || node.Kind != yaml.MappingNode {
		return condition
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := strings.TrimSpace(node.Content[i].Value)
		values := splitValues(node.Content[i+1].Value)
		if key != "" && len(values) > 0 {
			condition[key] = values
		}
	}
	return condition
}

func findMapValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.TrimSpace(node.Content[i].Value) == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func matchRule(rule Rule, item Item) (bool, error) {
	if len(rule.Condition) == 0 {
		return true, nil
	}
	for field, values := range rule.Condition {
		matched, err := matchField(field, values, item)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func matchField(field string, values []string, item Item) (bool, error) {
	switch field {
	case "genre_ids":
		return matchList(item.GenreIDs, values), nil
	case "original_language":
		return matchList([]string{item.OriginalLanguage}, values), nil
	case "origin_country":
		return matchList(item.OriginCountry, values), nil
	case "production_countries":
		return matchList(item.ProductionCountries, values), nil
	case "keywords":
		return matchKeywords(item.Keywords, values), nil
	default:
		return false, fmt.Errorf("分类字段 %s 不受支持", field)
	}
}

func validateConfig(cfg Config) error {
	for _, rule := range append(append([]Rule{}, cfg.Movie...), cfg.TV...) {
		for field := range rule.Condition {
			if !supportedField(field) {
				return fmt.Errorf("分类字段 %s 不受支持", field)
			}
		}
	}
	return nil
}

func supportedField(field string) bool {
	switch field {
	case "genre_ids", "original_language", "origin_country", "production_countries", "keywords":
		return true
	default:
		return false
	}
}

func matchList(actual []string, rules []string) bool {
	actualSet := map[string]struct{}{}
	for _, value := range actual {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			actualSet[value] = struct{}{}
		}
	}
	hasPositive := false
	positiveMatched := false
	for _, rule := range rules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if strings.HasPrefix(rule, "-") {
			if _, ok := actualSet[strings.TrimPrefix(rule, "-")]; ok {
				return false
			}
			continue
		}
		hasPositive = true
		if _, ok := actualSet[rule]; ok {
			positiveMatched = true
		}
	}
	return !hasPositive || positiveMatched
}

func matchKeywords(actual []string, rules []string) bool {
	if len(rules) == 0 {
		return true
	}
	text := strings.ToLower(strings.Join(actual, " "))
	hasPositive := false
	positiveMatched := false
	for _, rule := range rules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if strings.HasPrefix(rule, "-") {
			if keyword := strings.TrimSpace(strings.TrimPrefix(rule, "-")); keyword != "" && strings.Contains(text, keyword) {
				return false
			}
			continue
		}
		hasPositive = true
		if strings.Contains(text, rule) {
			positiveMatched = true
		}
	}
	return !hasPositive || positiveMatched
}

func splitValues(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
