package matcher

import (
	"regexp"
	"strings"
)

var nonWordRE = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func NormalizeTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("'", "", "’", "", "&", " and ").Replace(value)
	value = nonWordRE.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func ReleaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year := 0
	for _, r := range date[:4] {
		if r < '0' || r > '9' {
			return 0
		}
		year = year*10 + int(r-'0')
	}
	return year
}
