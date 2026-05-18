package parser

import (
	"fmt"
	"mime"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ElementKind string

const (
	ElementType             ElementKind = "type"
	ElementTitle            ElementKind = "title"
	ElementAlternativeTitle ElementKind = "alternative_title"
	ElementSeason           ElementKind = "season"
	ElementSeasonCount      ElementKind = "season_count"
	ElementEpisode          ElementKind = "episode"
	ElementEpisodeCount     ElementKind = "episode_count"
	ElementEpisodeTitle     ElementKind = "episode_title"
	ElementEpisodeDetail    ElementKind = "episode_detail"
	ElementEpisodeFormat    ElementKind = "episode_format"
	ElementPart             ElementKind = "part"
	ElementDisc             ElementKind = "disc"
	ElementCD               ElementKind = "cd"
	ElementVolume           ElementKind = "volume"
	ElementYear             ElementKind = "year"
	ElementDate             ElementKind = "date"
	ElementWeek             ElementKind = "week"
	ElementContainer        ElementKind = "container"
	ElementMimeType         ElementKind = "mimetype"
	ElementWebsite          ElementKind = "website"
	ElementStreamingService ElementKind = "streaming_service"
	ElementCountry          ElementKind = "country"
	ElementLanguage         ElementKind = "language"
	ElementSubtitleLanguage ElementKind = "subtitle_language"
	ElementSource           ElementKind = "source"
	ElementResolution       ElementKind = "screen_size"
	ElementVideoCodec       ElementKind = "video_codec"
	ElementVideoProfile     ElementKind = "video_profile"
	ElementColorDepth       ElementKind = "color_depth"
	ElementVideoAPI         ElementKind = "video_api"
	ElementVideoBitRate     ElementKind = "video_bit_rate"
	ElementFrameRate        ElementKind = "frame_rate"
	ElementAudioCodec       ElementKind = "audio_codec"
	ElementAudioProfile     ElementKind = "audio_profile"
	ElementAudioBitRate     ElementKind = "audio_bit_rate"
	ElementAudioChannels    ElementKind = "audio_channels"
	ElementEdition          ElementKind = "edition"
	ElementOther            ElementKind = "other"
	ElementReleaseGroup     ElementKind = "release_group"
	ElementReleaseVersion   ElementKind = "version"
	ElementChecksum         ElementKind = "crc32"
	ElementUUID             ElementKind = "uuid"
	ElementSize             ElementKind = "size"
	ElementBonus            ElementKind = "bonus"
	ElementBonusTitle       ElementKind = "bonus_title"
)

type Element struct {
	Kind     ElementKind
	Value    string
	Position int
}

type namedAlias struct {
	Canonical string
	Aliases   []string
}

var (
	richResolutionRE   = regexp.MustCompile(`(?i)\b(4320p|2160p|1440p|1080p|1080i|900p|900i|720p|576p|576i|540p|540i|480p|480i|368p|360p|360i|4k|8k|7680x4320|3840x2160|2560x1440|1920x1080|1600x900|1280x720|1024x576|960x540|854x480|852x480|720x480|640x360)\b`)
	richSourceRE       = regexp.MustCompile(`(?i)\b(Ultra[ ._-]?HD[ ._-]?Blu[ ._-]?ray|Blu[ ._-]?ray|HD[ ._-]?DVD|WEB[ ._-]?DL|WEBRip|WEB|Video[ ._-]?on[ ._-]?Demand|VOD|HDTV|UHDTV|DVD|DVDRip|BDRip|HDRip|Remux|Satellite|PPV|Pay[ ._-]?Per[ ._-]?View|TV|Workprint|Telecine|Telesync|Camera)\b`)
	richVideoCodecRE   = regexp.MustCompile(`(?i)\b(xvid|divx|realvideo|rv\d+|x264|x265|x266|h[ ._-]?263|h[ ._-]?264|h[ ._-]?265|h[ ._-]?266|hevc|avc|vvc|vp7|vp8|vp9|av1|mpeg[ ._-]?2)\b`)
	richAudioCodecRE   = regexp.MustCompile(`(?i)\b(TrueHD|Atmos|E[ ._-]?AC[ ._-]?3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DDP(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DD\+(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|AC[ ._-]?3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD[ ._-]?MA(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTSHD[ ._-]?MA(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD[ ._-]?HR(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTSHD[ ._-]?HR(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?X|DTS(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|AAC(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|FLAC|LPCM|PCM|Opus|Vorbis|MP3|MP2)\b`)
	videoProfileRE     = regexp.MustCompile(`(?i)\b(Hi10P|Main10|Baseline|Main|High(?:[ ._-]?10|[ ._-]?4:2:2|[ ._-]?4:4:4(?:[ ._-]?Predictive)?)?|Extended|SVC|AVCHD|HEVC)\b`)
	colorDepthRE       = regexp.MustCompile(`(?i)\b(8|10|12)-?bit\b`)
	videoAPIRE         = regexp.MustCompile(`(?i)\b(DXVA)\b`)
	bitRateRE          = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s?(?:Kbps|Mbps)\b`)
	frameRateRE        = regexp.MustCompile(`(?i)\b(23\.976|24(?:\.0{1,3})?|25(?:\.0{1,3})?|29\.970|30(?:\.0{1,3})?|48(?:\.0{1,3})?|50(?:\.0{1,3})?|60(?:\.0{1,3})?|120(?:\.0{1,3})?)\s?fps\b`)
	sizeRE             = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?\s?(?:KB|MB|GB|TB)\b`)
	uuidRE             = regexp.MustCompile(`(?i)\b[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\b`)
	websiteRE          = regexp.MustCompile(`(?i)\b(?:www\.)?[a-z0-9][a-z0-9-]{0,62}\.(?:com|net|org|io|me|co|tv|uk|ru|be|to|cc|ws|info|biz|xyz|fm|ly)\b`)
	weekRE             = regexp.MustCompile(`(?i)\bW(?:EEK)?[ ._-]?(\d{1,2})\b`)
	generalDateRE      = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])((?:19|20)\d{2})[ ._\-](\d{2})[ ._\-](\d{2})(?:$|[ ._\-\]\)])`)
	countryRE          = regexp.MustCompile(`(?i)[\(\[]\s*(US|UK|GB|AU|CA|JP|KR|CN|TW|HK)\s*[\)\]]`)
	seasonCountRE      = regexp.MustCompile(`(?i)\bS(\d{1,2})[ ._-]*-[ ._-]*S?(\d{1,2})\b`)
	episodeOfCountRE   = regexp.MustCompile(`(?i)\b(?:E|EP)?(\d{1,3})[ ._-]*(?:OF|/)[ ._-]*(\d{1,3})\b`)
	episodeWordCountRE = regexp.MustCompile(`(?i)\b(\d{1,3})\s*episodes?\b`)
	partRE             = regexp.MustCompile(`(?i)\b(?:pt|part)[ ._-]?(\d{1,2})\b`)
	discRE             = regexp.MustCompile(`(?i)\b(?:disc|d)[ ._-]?(\d{1,2})(?:[ ._-]*(?:of|/)[ ._-]*(\d{1,2}))?\b`)
	cdRE               = regexp.MustCompile(`(?i)\bcd[ ._-]?(\d{1,2})(?:[ ._-]*(?:of|/)[ ._-]*(\d{1,2}))?\b`)
	cdCountRE          = regexp.MustCompile(`(?i)\b(\d{1,2})[ ._-]?cds?\b`)
	bonusRE            = regexp.MustCompile(`(?i)\bbonus[ ._-]?(\d{1,2})\b`)
	bonusTitleRE       = regexp.MustCompile(`(?i)\bbonus[ ._-]?\d{1,2}[ ._\-:]+(.+)$`)
	volumeRE           = regexp.MustCompile(`(?i)\bvol(?:ume)?[ ._-]?(\d{1,3})(?:[ ._-]*[-~][ ._-]*(\d{1,3}))?\b`)
	episodeDetailRE    = regexp.MustCompile(`(?i)\b(Pilot|Final|Special|Unaired)\b`)
	episodeFormatRE    = regexp.MustCompile(`(?i)\b(Minisode)\b`)
)

var mimeOverrides = map[string]string{
	"mkv":  "video/x-matroska",
	"mk3d": "video/x-matroska",
	"mka":  "audio/x-matroska",
	"mp4":  "video/mp4",
	"m4v":  "video/x-m4v",
	"avi":  "video/x-msvideo",
	"mov":  "video/quicktime",
	"ts":   "video/mp2t",
	"m2ts": "video/mp2t",
	"iso":  "application/x-iso9660-image",
	"srt":  "application/x-subrip",
	"ass":  "text/x-ssa",
	"ssa":  "text/x-ssa",
	"sub":  "text/plain",
	"nfo":  "text/plain",
}

var streamingServices = []namedAlias{
	{Canonical: "Paramount+", Aliases: []string{"PMTP", "PMNP", "PMT+", "PARAMOUNT+", "PARAMOUNTPLUS"}},
	{Canonical: "Amazon Prime", Aliases: []string{"AMZN-CBR", "AMZN", "AMAZON PRIME", "AMAZON"}},
	{Canonical: "Crunchy Roll", Aliases: []string{"CRUNCHYROLL", "CRUNCHY ROLL", "CR"}},
	{Canonical: "AppleTV", Aliases: []string{"APPLETV", "ATVP", "ATV+", "APTV"}},
	{Canonical: "Disney+", Aliases: []string{"DISNEY+", "DSNP"}},
	{Canonical: "BBC iPlayer", Aliases: []string{"BBC IPLAYER", "IPLAYER", "IP"}},
	{Canonical: "Comedy Central", Aliases: []string{"COMEDY CENTRAL", "CC"}},
	{Canonical: "National Geographic", Aliases: []string{"NATIONAL GEOGRAPHIC", "NATG"}},
	{Canonical: "Netflix", Aliases: []string{"NETFLIX", "NF"}},
	{Canonical: "HBO Max", Aliases: []string{"HBO MAX", "HMAX", "MAX"}},
	{Canonical: "HBO Go", Aliases: []string{"HBO GO", "HBO"}},
	{Canonical: "Discovery Plus", Aliases: []string{"DISCOVERY PLUS", "DSCP"}},
	{Canonical: "Discovery", Aliases: []string{"DISCOVERY", "DISC"}},
	{Canonical: "Paramount+", Aliases: []string{"PARAMOUNT+"}},
	{Canonical: "Peacock", Aliases: []string{"PEACOCK", "PCOK"}},
	{Canonical: "Hulu", Aliases: []string{"HULU"}},
	{Canonical: "Disney", Aliases: []string{"DISNEY", "DSNY"}},
	{Canonical: "Showtime", Aliases: []string{"SHOWTIME", "SHO"}},
	{Canonical: "The CW", Aliases: []string{"THE CW", "CW"}},
	{Canonical: "BBC iPlayer", Aliases: []string{"BBC-IPLAYER"}},
	{Canonical: "iQIYI", Aliases: []string{"IQIYI"}},
	{Canonical: "Bilibili", Aliases: []string{"BILIBILI"}},
	{Canonical: "Viki", Aliases: []string{"VIKI"}},
	{Canonical: "Viu", Aliases: []string{"VIU"}},
	{Canonical: "TVING", Aliases: []string{"TVING"}},
	{Canonical: "Stan", Aliases: []string{"STAN"}},
	{Canonical: "Canal+", Aliases: []string{"CANAL+"}},
	{Canonical: "YouTube Red", Aliases: []string{"YOUTUBE RED", "RED"}},
	{Canonical: "iTunes", Aliases: []string{"ITUNES"}},
	{Canonical: "NBC", Aliases: []string{"NBC"}},
	{Canonical: "CBS", Aliases: []string{"CBS"}},
	{Canonical: "ABC", Aliases: []string{"ABC"}},
}

var languageAliases = map[string]string{
	"en":       "English",
	"eng":      "English",
	"english":  "English",
	"fr":       "French",
	"fra":      "French",
	"fre":      "French",
	"french":   "French",
	"vf":       "French",
	"es":       "Spanish",
	"spa":      "Spanish",
	"spanish":  "Spanish",
	"espanol":  "Spanish",
	"de":       "German",
	"deu":      "German",
	"ger":      "German",
	"german":   "German",
	"it":       "Italian",
	"ita":      "Italian",
	"italian":  "Italian",
	"pt":       "Portuguese",
	"por":      "Portuguese",
	"ptbr":     "Brazilian Portuguese",
	"pob":      "Brazilian Portuguese",
	"pb":       "Brazilian Portuguese",
	"br":       "Brazilian Portuguese",
	"ru":       "Russian",
	"rus":      "Russian",
	"russian":  "Russian",
	"ja":       "Japanese",
	"jp":       "Japanese",
	"jpn":      "Japanese",
	"japanese": "Japanese",
	"ko":       "Korean",
	"kor":      "Korean",
	"korean":   "Korean",
	"zh":       "Chinese",
	"chi":      "Chinese",
	"zho":      "Chinese",
	"cn":       "Chinese",
	"chs":      "Chinese (Simplified)",
	"sc":       "Chinese (Simplified)",
	"hans":     "Chinese (Simplified)",
	"gb":       "Chinese (Simplified)",
	"gbk":      "Chinese (Simplified)",
	"cht":      "Chinese (Traditional)",
	"tc":       "Chinese (Traditional)",
	"hant":     "Chinese (Traditional)",
	"big5":     "Chinese (Traditional)",
	"tw":       "Chinese (Traditional)",
	"hk":       "Chinese (Traditional)",
	"ar":       "Arabic",
	"ara":      "Arabic",
	"th":       "Thai",
	"tha":      "Thai",
	"vi":       "Vietnamese",
	"vie":      "Vietnamese",
	"id":       "Indonesian",
	"ind":      "Indonesian",
	"pl":       "Polish",
	"pol":      "Polish",
	"nl":       "Dutch",
	"dut":      "Dutch",
	"nld":      "Dutch",
	"sv":       "Swedish",
	"swe":      "Swedish",
	"no":       "Norwegian",
	"nor":      "Norwegian",
	"fi":       "Finnish",
	"fin":      "Finnish",
	"hi":       "Hindi",
	"hin":      "Hindi",
	"mul":      "Multiple Languages",
	"multi":    "Multiple Languages",
	"multiple": "Multiple Languages",
}

var otherAliases = []namedAlias{
	{Canonical: "Proper", Aliases: []string{"PROPER", "REPACK", "RERIP", "REAL PROPER"}},
	{Canonical: "Sample", Aliases: []string{"SAMPLE"}},
	{Canonical: "Trailer", Aliases: []string{"TRAILER"}},
	{Canonical: "Extras", Aliases: []string{"EXTRAS", "DIGITAL EXTRAS"}},
	{Canonical: "Complete", Aliases: []string{"COMPLETE"}},
	{Canonical: "Dual Audio", Aliases: []string{"DUAL AUDIO", "DUAL"}},
	{Canonical: "Documentary", Aliases: []string{"DOCU", "DOKU", "DOCUMENTARY"}},
	{Canonical: "Open Matte", Aliases: []string{"OPEN MATTE", "OM"}},
	{Canonical: "3D", Aliases: []string{"3D"}},
	{Canonical: "High Frame Rate", Aliases: []string{"HFR"}},
	{Canonical: "Variable Frame Rate", Aliases: []string{"VFR"}},
	{Canonical: "Micro HD", Aliases: []string{"MHD", "HDLIGHT"}},
	{Canonical: "HDR10", Aliases: []string{"HDR10", "HDR"}},
	{Canonical: "Dolby Vision", Aliases: []string{"DOLBY VISION", "DOVI", "DV"}},
	{Canonical: "Standard Dynamic Range", Aliases: []string{"SDR"}},
	{Canonical: "Bonus", Aliases: []string{"BONUS"}},
	{Canonical: "Retail", Aliases: []string{"RETAIL"}},
	{Canonical: "Internal", Aliases: []string{"INTERNAL"}},
}

var customCountryNames = map[string]string{
	"US": "United States",
	"UK": "United Kingdom",
	"GB": "United Kingdom",
	"AU": "Australia",
	"CA": "Canada",
	"JP": "Japan",
	"KR": "South Korea",
	"CN": "China",
	"TW": "Taiwan",
	"HK": "Hong Kong",
}

var subtitleContextTerms = []string{
	"sub", "subs", "subtitle", "subtitles", "subbed", "fansub", "hardsub", "softsub",
	"softsubs", "soft subtitles", "vost", "vostfr", "字幕", "内封字幕", "外挂字幕", "简中", "繁中",
}

var audioContextTerms = []string{
	"audio", "dub", "dubbed", "dublado", "dual audio", "multiple audio",
}

func finalizeRichResult(result *Result, parts []string, base, parentTitle, sourceText string) {
	if result == nil {
		return
	}
	if result.Type == "" {
		if result.IsTV {
			result.Type = "episode"
		} else {
			result.Type = "movie"
		}
	}
	if result.Container == "" {
		result.Container = result.Extension
	}
	if result.MimeType == "" {
		result.MimeType = guessMimeType(result.Container)
	}
	if result.Date == "" {
		if strings.TrimSpace(result.AirDate) != "" {
			result.Date = strings.TrimSpace(result.AirDate)
		} else if date := detectGeneralDate(sourceText); date != "" {
			result.Date = date
		}
	}
	if result.CRC32 == "" && result.Checksum != "" {
		result.CRC32 = strings.ToUpper(result.Checksum)
	}
	if result.AlternativeTitles == nil {
		result.AlternativeTitles = deriveAlternativeTitles(*result, parentTitle, base)
	}

	enrichReleaseTokens(result, sourceText)
	detectWebsite(result, sourceText)
	detectStreamingService(result, sourceText)
	detectLocalization(result, parts, base, parentTitle, sourceText)
	detectCountsAndParts(result, sourceText)
	detectEpisodeMetadata(result, base)
	detectOther(result, sourceText)
	collectCoreElements(result, base, sourceText)
	sortElements(result)
}

func guessMimeType(container string) string {
	container = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(container)), ".")
	if container == "" {
		return ""
	}
	if value, ok := mimeOverrides[container]; ok {
		return value
	}
	return mime.TypeByExtension("." + container)
}

func detectGeneralDate(value string) string {
	match := generalDateRE.FindStringSubmatchIndex(value)
	if match == nil || len(match) < 8 {
		return ""
	}
	year := firstMatch(value, match, 2, 3)
	month := firstMatch(value, match, 4, 5)
	day := firstMatch(value, match, 6, 7)
	date := fmt.Sprintf("%s-%s-%s", year, month, day)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return ""
	}
	return date
}

func deriveAlternativeTitles(result Result, parentTitle, base string) []string {
	primary := strings.TrimSpace(result.Title)
	if result.IsTV {
		primary = strings.TrimSpace(result.ShowTitle)
	}
	candidates := uniqueNonEmpty(append([]string{}, result.SearchTitles...)...)
	candidates = append(candidates, localizedTitleCandidates(parentTitle)...)
	candidates = append(candidates, localizedTitleCandidates(base)...)
	values := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	primaryKey := strings.ToLower(primary)
	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" || key == primaryKey {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, candidate)
	}
	return values
}

func enrichReleaseTokens(result *Result, value string) {
	if result == nil {
		return
	}
	if unknownRichValue(result.Resolution) {
		if token := findToken(richResolutionRE, value); token != "" {
			result.Resolution = canonicalResolution(token)
		}
	}
	if unknownRichValue(result.Source) {
		if token := findToken(richSourceRE, value); token != "" {
			result.Source = canonicalRichSource(token)
		}
	}
	if unknownRichValue(result.VideoCodec) {
		if token := findToken(richVideoCodecRE, value); token != "" {
			result.VideoCodec = canonicalRichVideoCodec(token)
		}
	}
	if unknownRichValue(result.AudioCodec) {
		if token := findToken(richAudioCodecRE, value); token != "" {
			result.AudioCodec = canonicalAudioCodec(token)
		}
	}
	if result.AudioProfile == "" {
		result.AudioProfile = audioProfileFromText(value)
	}
	if unknownRichValue(result.AudioChannels) {
		if token := findToken(audioChannelsRE, value); token != "" {
			result.AudioChannels = canonicalAudioChannels(token)
		}
	}
	if result.VideoProfile == "" {
		if token := findToken(videoProfileRE, value); token != "" {
			result.VideoProfile = canonicalVideoProfile(token)
		}
	}
	if result.ColorDepth == "" {
		if token := findToken(colorDepthRE, value); token != "" {
			result.ColorDepth = strings.TrimSpace(token) + "-bit"
		}
	}
	if result.VideoAPI == "" {
		if token := findToken(videoAPIRE, value); token != "" {
			result.VideoAPI = strings.ToUpper(strings.TrimSpace(token))
		}
	}
	if result.VideoBitRate == "" {
		for _, match := range bitRateRE.FindAllString(value, -1) {
			if strings.Contains(strings.ToLower(match), "mbps") {
				result.VideoBitRate = normalizeCompactValue(match)
				break
			}
		}
	}
	if result.AudioBitRate == "" {
		for _, match := range bitRateRE.FindAllString(value, -1) {
			if strings.Contains(strings.ToLower(match), "kbps") {
				result.AudioBitRate = normalizeCompactValue(match)
				break
			}
		}
	}
	if result.FrameRate == "" {
		if match := frameRateRE.FindStringSubmatch(value); len(match) > 1 {
			result.FrameRate = strings.TrimSpace(match[1]) + "fps"
		}
	}
	if result.Size == "" {
		if match := sizeRE.FindString(value); match != "" {
			result.Size = normalizeCompactValue(match)
		}
	}
	if result.UUID == "" {
		if match := uuidRE.FindString(value); match != "" {
			result.UUID = strings.ToUpper(match)
		}
	}
	if result.CRC32 == "" {
		if match := checksumRE.FindStringSubmatch(value); len(match) > 1 {
			result.CRC32 = strings.ToUpper(match[1])
		}
	}
}

func detectWebsite(result *Result, value string) {
	if result == nil || result.Website != "" {
		return
	}
	match := websiteRE.FindString(value)
	if match == "" {
		return
	}
	result.Website = strings.TrimSpace(match)
	addElement(result, ElementWebsite, result.Website, findPositionInsensitive(value, match))
}

func detectStreamingService(result *Result, value string) {
	if result == nil || result.StreamingService != "" {
		return
	}
	normalized := normalizeAliasText(value)
	bestPos := -1
	bestLen := -1
	bestName := ""
	bestRaw := ""
	for _, service := range streamingServices {
		for _, alias := range service.Aliases {
			pos := findAliasPosition(normalized, alias)
			if pos < 0 {
				continue
			}
			if aliasLen := len(alias); aliasLen > bestLen || (aliasLen == bestLen && (bestPos < 0 || pos < bestPos)) {
				bestLen = aliasLen
				bestPos = pos
				bestName = service.Canonical
				bestRaw = alias
			}
		}
	}
	if bestName == "" {
		return
	}
	result.StreamingService = bestName
	addElement(result, ElementStreamingService, bestName, findPositionInsensitive(normalized, bestRaw))
}

func detectLocalization(result *Result, parts []string, base, parentTitle, sourceText string) {
	if result == nil {
		return
	}
	for _, match := range countryRE.FindAllStringSubmatch(sourceText, -1) {
		if len(match) < 2 {
			continue
		}
		code := strings.ToUpper(match[1])
		name := customCountryNames[code]
		if name == "" {
			name = code
		}
		result.Countries = appendUniqueString(result.Countries, name)
		addElement(result, ElementCountry, name, findPositionInsensitive(sourceText, match[0]))
	}

	segments := collectMetadataSegments(parts, base, parentTitle, sourceText)
	for _, segment := range segments {
		segmentLower := strings.ToLower(segment)
		subtitleContext := containsAnyTerm(segmentLower, subtitleContextTerms)
		audioContext := containsAnyTerm(segmentLower, audioContextTerms)
		for _, piece := range splitMetadataPiece(segment) {
			language, ok := canonicalLanguageToken(piece)
			if !ok {
				continue
			}
			switch {
			case subtitleContext || isSubtitleLikeLanguageToken(piece):
				result.SubtitleLanguages = appendUniqueString(result.SubtitleLanguages, language)
				addElement(result, ElementSubtitleLanguage, language, findPositionInsensitive(sourceText, piece))
			case audioContext || language != "Chinese (Simplified)" && language != "Chinese (Traditional)":
				result.Languages = appendUniqueString(result.Languages, language)
				addElement(result, ElementLanguage, language, findPositionInsensitive(sourceText, piece))
			default:
				result.SubtitleLanguages = appendUniqueString(result.SubtitleLanguages, language)
				addElement(result, ElementSubtitleLanguage, language, findPositionInsensitive(sourceText, piece))
			}
		}
	}
}

func detectCountsAndParts(result *Result, value string) {
	if result == nil {
		return
	}
	if result.Week == 0 {
		if match := weekRE.FindStringSubmatch(value); len(match) > 1 {
			result.Week = atoi(match[1])
		}
	}
	if result.SeasonCount == 0 {
		if match := seasonCountRE.FindStringSubmatch(value); len(match) > 2 {
			start := atoi(match[1])
			end := atoi(match[2])
			if start > 0 && end >= start {
				result.SeasonCount = end - start + 1
			}
		}
	}
	if result.EpisodeCount == 0 {
		if len(result.Episodes) > 1 {
			result.EpisodeCount = len(result.Episodes)
		} else if match := episodeOfCountRE.FindStringSubmatch(value); len(match) > 2 {
			result.EpisodeCount = atoi(match[2])
		} else if match := episodeWordCountRE.FindStringSubmatch(value); len(match) > 1 {
			result.EpisodeCount = atoi(match[1])
		}
	}
	if result.Part == 0 {
		if match := partRE.FindStringSubmatch(value); len(match) > 1 {
			result.Part = atoi(match[1])
		}
	}
	if result.Disc == 0 {
		if match := discRE.FindStringSubmatch(value); len(match) > 1 {
			result.Disc = atoi(match[1])
			if len(match) > 2 {
				result.DiscCount = atoi(match[2])
			}
		}
	}
	if result.CD == 0 {
		if match := cdRE.FindStringSubmatch(value); len(match) > 1 {
			result.CD = atoi(match[1])
			if len(match) > 2 {
				result.CDCount = atoi(match[2])
			}
		} else if match := cdCountRE.FindStringSubmatch(value); len(match) > 1 {
			result.CDCount = atoi(match[1])
		}
	}
	if result.Bonus == 0 {
		if match := bonusRE.FindStringSubmatch(value); len(match) > 1 {
			result.Bonus = atoi(match[1])
		}
	}
	if result.BonusTitle == "" {
		if match := bonusTitleRE.FindStringSubmatch(value); len(match) > 1 {
			result.BonusTitle = cleanTitle(match[1])
		}
	}
	if len(result.Volume) == 0 {
		for _, match := range volumeRE.FindAllStringSubmatch(value, -1) {
			if len(match) > 1 {
				start := atoi(match[1])
				end := start
				if len(match) > 2 && match[2] != "" {
					end = atoi(match[2])
				}
				if start > 0 && end >= start && end-start <= 20 {
					for current := start; current <= end; current++ {
						result.Volume = appendUniqueInt(result.Volume, current)
					}
				}
			}
		}
	}
}

func detectEpisodeMetadata(result *Result, base string) {
	if result == nil {
		return
	}
	if len(result.EpisodeDetails) == 0 {
		for _, match := range episodeDetailRE.FindAllStringSubmatch(base, -1) {
			if len(match) > 1 {
				result.EpisodeDetails = appendUniqueString(result.EpisodeDetails, canonicalEpisodeDetail(match[1]))
			}
		}
	}
	if result.EpisodeFormat == "" {
		if match := episodeFormatRE.FindStringSubmatch(base); len(match) > 1 {
			result.EpisodeFormat = canonicalEpisodeFormat(match[1])
		}
	}
	if result.EpisodeTitle == "" && result.IsTV {
		if title := detectEpisodeTitle(base, result); title != "" {
			result.EpisodeTitle = title
		}
	}
}

func detectOther(result *Result, value string) {
	if result == nil {
		return
	}
	normalized := normalizeAliasText(value)
	for _, item := range otherAliases {
		for _, alias := range item.Aliases {
			if findAliasPosition(normalized, alias) >= 0 {
				result.Other = appendUniqueString(result.Other, item.Canonical)
				break
			}
		}
	}
}

func collectCoreElements(result *Result, base, sourceText string) {
	if result == nil {
		return
	}
	addElement(result, ElementType, result.Type, 0)
	if result.IsTV {
		addElement(result, ElementTitle, result.ShowTitle, findPositionInsensitive(sourceText, result.ShowTitle))
	} else {
		addElement(result, ElementTitle, result.Title, findPositionInsensitive(sourceText, result.Title))
	}
	for _, value := range result.AlternativeTitles {
		addElement(result, ElementAlternativeTitle, value, findPositionInsensitive(sourceText, value))
	}
	if result.Year > 0 && !result.IsTV {
		addElement(result, ElementYear, strconv.Itoa(result.Year), findPositionInsensitive(base, strconv.Itoa(result.Year)))
	}
	if result.ShowYear > 0 && result.IsTV {
		addElement(result, ElementYear, strconv.Itoa(result.ShowYear), findPositionInsensitive(base, strconv.Itoa(result.ShowYear)))
	}
	if result.Season > 0 || result.IsTV {
		addElement(result, ElementSeason, strconv.Itoa(result.Season), findPositionInsensitive(base, "S"+result.Season2))
	}
	if result.SeasonCount > 0 {
		addElement(result, ElementSeasonCount, strconv.Itoa(result.SeasonCount), findPositionInsensitive(sourceText, strconv.Itoa(result.SeasonCount)))
	}
	if result.Episode > 0 || result.IsTV {
		addElement(result, ElementEpisode, strconv.Itoa(result.Episode), findPositionInsensitive(base, "E"+result.Episode2))
	}
	if result.EpisodeCount > 0 {
		addElement(result, ElementEpisodeCount, strconv.Itoa(result.EpisodeCount), findPositionInsensitive(sourceText, strconv.Itoa(result.EpisodeCount)))
	}
	addElement(result, ElementEpisodeTitle, result.EpisodeTitle, findPositionInsensitive(base, result.EpisodeTitle))
	for _, detail := range result.EpisodeDetails {
		addElement(result, ElementEpisodeDetail, detail, findPositionInsensitive(base, detail))
	}
	addElement(result, ElementEpisodeFormat, result.EpisodeFormat, findPositionInsensitive(base, result.EpisodeFormat))
	if result.Part > 0 {
		addElement(result, ElementPart, strconv.Itoa(result.Part), findPositionInsensitive(base, strconv.Itoa(result.Part)))
	}
	if result.Disc > 0 {
		addElement(result, ElementDisc, strconv.Itoa(result.Disc), findPositionInsensitive(base, strconv.Itoa(result.Disc)))
	}
	if result.CD > 0 {
		addElement(result, ElementCD, strconv.Itoa(result.CD), findPositionInsensitive(base, strconv.Itoa(result.CD)))
	}
	for _, volume := range result.Volume {
		addElement(result, ElementVolume, strconv.Itoa(volume), findPositionInsensitive(base, strconv.Itoa(volume)))
	}
	addElement(result, ElementDate, result.Date, findPositionInsensitive(sourceText, result.Date))
	if result.Week > 0 {
		addElement(result, ElementWeek, strconv.Itoa(result.Week), findPositionInsensitive(sourceText, strconv.Itoa(result.Week)))
	}
	addElement(result, ElementContainer, result.Container, findPositionInsensitive(sourceText, result.Container))
	addElement(result, ElementMimeType, result.MimeType, 0)
	addElement(result, ElementStreamingService, result.StreamingService, findPositionInsensitive(sourceText, result.StreamingService))
	addElement(result, ElementWebsite, result.Website, findPositionInsensitive(sourceText, result.Website))
	for _, country := range result.Countries {
		addElement(result, ElementCountry, country, findPositionInsensitive(sourceText, country))
	}
	for _, language := range result.Languages {
		addElement(result, ElementLanguage, language, findPositionInsensitive(sourceText, language))
	}
	for _, language := range result.SubtitleLanguages {
		addElement(result, ElementSubtitleLanguage, language, findPositionInsensitive(sourceText, language))
	}
	addElement(result, ElementSource, result.Source, findPositionInsensitive(sourceText, result.Source))
	addElement(result, ElementResolution, result.Resolution, findPositionInsensitive(sourceText, result.Resolution))
	addElement(result, ElementVideoCodec, result.VideoCodec, findPositionInsensitive(sourceText, result.VideoCodec))
	addElement(result, ElementVideoProfile, result.VideoProfile, findPositionInsensitive(sourceText, result.VideoProfile))
	addElement(result, ElementColorDepth, result.ColorDepth, findPositionInsensitive(sourceText, result.ColorDepth))
	addElement(result, ElementVideoAPI, result.VideoAPI, findPositionInsensitive(sourceText, result.VideoAPI))
	addElement(result, ElementVideoBitRate, result.VideoBitRate, findPositionInsensitive(sourceText, result.VideoBitRate))
	addElement(result, ElementFrameRate, result.FrameRate, findPositionInsensitive(sourceText, result.FrameRate))
	addElement(result, ElementAudioCodec, result.AudioCodec, findPositionInsensitive(sourceText, result.AudioCodec))
	addElement(result, ElementAudioProfile, result.AudioProfile, findPositionInsensitive(sourceText, result.AudioProfile))
	addElement(result, ElementAudioBitRate, result.AudioBitRate, findPositionInsensitive(sourceText, result.AudioBitRate))
	addElement(result, ElementAudioChannels, result.AudioChannels, findPositionInsensitive(sourceText, result.AudioChannels))
	addElement(result, ElementEdition, result.Edition, findPositionInsensitive(sourceText, result.Edition))
	addElement(result, ElementReleaseGroup, result.ReleaseGroup, findPositionInsensitive(sourceText, result.ReleaseGroup))
	if result.ReleaseVersion > 0 {
		addElement(result, ElementReleaseVersion, strconv.Itoa(result.ReleaseVersion), findPositionInsensitive(base, "v"+strconv.Itoa(result.ReleaseVersion)))
	}
	addElement(result, ElementChecksum, result.CRC32, findPositionInsensitive(sourceText, result.CRC32))
	addElement(result, ElementUUID, result.UUID, findPositionInsensitive(sourceText, result.UUID))
	addElement(result, ElementSize, result.Size, findPositionInsensitive(sourceText, result.Size))
	if result.Bonus > 0 {
		addElement(result, ElementBonus, strconv.Itoa(result.Bonus), findPositionInsensitive(sourceText, strconv.Itoa(result.Bonus)))
	}
	addElement(result, ElementBonusTitle, result.BonusTitle, findPositionInsensitive(sourceText, result.BonusTitle))
	for _, other := range result.Other {
		addElement(result, ElementOther, other, findPositionInsensitive(sourceText, other))
	}
}

func addElement(result *Result, kind ElementKind, value string, position int) {
	if result == nil || strings.TrimSpace(value) == "" {
		return
	}
	for _, element := range result.Elements {
		if element.Kind == kind && strings.EqualFold(element.Value, value) {
			return
		}
	}
	result.Elements = append(result.Elements, Element{
		Kind:     kind,
		Value:    strings.TrimSpace(value),
		Position: position,
	})
}

func sortElements(result *Result) {
	if result == nil || len(result.Elements) == 0 {
		return
	}
	sort.SliceStable(result.Elements, func(i, j int) bool {
		left := result.Elements[i]
		right := result.Elements[j]
		leftPos := left.Position
		rightPos := right.Position
		if leftPos < 0 {
			leftPos = 1 << 30
		}
		if rightPos < 0 {
			rightPos = 1 << 30
		}
		if leftPos != rightPos {
			return leftPos < rightPos
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return strings.ToLower(left.Value) < strings.ToLower(right.Value)
	})
}

func detectEpisodeTitle(base string, result *Result) string {
	if result == nil || !result.IsTV {
		return ""
	}
	patterns := []string{
		fmt.Sprintf(`(?i)S%02d[ ._-]*E%02d(?:v\d+)?[ ._\-:]+(.+)$`, result.Season, result.Episode),
		fmt.Sprintf(`(?i)\b%d[ ._\-:]+(.+)$`, result.Episode),
		fmt.Sprintf(`(?i)\b%02d(?:v\d+)?[ ._\-:]+(.+)$`, result.Episode),
	}
	for _, rawPattern := range patterns {
		re := regexp.MustCompile(rawPattern)
		match := re.FindStringSubmatch(base)
		if len(match) < 2 {
			continue
		}
		title := trimTechnicalTail(match[1])
		title = cleanTitle(title)
		if title == "" {
			continue
		}
		if strings.EqualFold(title, result.ShowTitle) || looksLikeEpisodeContinuationTitle(title) {
			continue
		}
		return title
	}
	return ""
}

func trimTechnicalTail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	candidates := []int{}
	for _, re := range []*regexp.Regexp{
		richResolutionRE, richSourceRE, richVideoCodecRE, richAudioCodecRE,
		bitRateRE, frameRateRE, colorDepthRE, checksumRE, websiteRE,
	} {
		if loc := re.FindStringIndex(value); loc != nil {
			candidates = append(candidates, loc[0])
		}
	}
	if len(candidates) > 0 {
		sort.Ints(candidates)
		value = value[:candidates[0]]
	}
	return strings.Trim(value, " ._-[]()")
}

func unknownRichValue(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "Unknown")
}

func canonicalRichSource(value string) string {
	normalized := normalizeAliasText(value)
	switch normalized {
	case "ULTRA HD BLU RAY", "ULTRAHDBLURAY":
		return "Ultra HD Blu-ray"
	case "BLU RAY", "BLURAY":
		return "Blu-ray"
	case "HD DVD", "HDDVD":
		return "HD-DVD"
	case "WEB DL":
		return "WEB-DL"
	case "WEBRIP":
		return "WEBRip"
	case "WEB", "VOD", "VIDEO ON DEMAND":
		return "Web"
	case "UHDTV":
		return "Ultra HDTV"
	case "DVDRIP", "DVD":
		return "DVD"
	case "PPV", "PAY PER VIEW":
		return "Pay-per-view"
	default:
		return strings.TrimSpace(value)
	}
}

func canonicalRichVideoCodec(value string) string {
	switch strings.ToUpper(strings.ReplaceAll(canonicalToken(value), "-", "")) {
	case "XVID":
		return "Xvid"
	case "DIVX":
		return "DivX"
	case "H263":
		return "H.263"
	case "H264", "X264", "AVC":
		return "H.264"
	case "H265", "X265", "HEVC":
		return "H.265"
	case "H266", "X266", "VVC":
		return "H.266"
	case "MPEG2":
		return "MPEG-2"
	case "VP7":
		return "VP7"
	case "VP8":
		return "VP8"
	case "VP9":
		return "VP9"
	case "REALVIDEO", "RV9", "RV10", "RV40":
		return "RealVideo"
	default:
		return strings.TrimSpace(value)
	}
}

func canonicalVideoProfile(value string) string {
	normalized := normalizeAliasText(value)
	switch normalized {
	case "HI10P", "HIGH 10":
		return "High 10"
	case "BASELINE":
		return "Baseline"
	case "MAIN":
		return "Main"
	case "MAIN10":
		return "Main 10"
	case "HIGH":
		return "High"
	case "HIGH 4 2 2":
		return "High 4:2:2"
	case "HIGH 4 4 4 PREDICTIVE", "HIGH 4 4 4":
		return "High 4:4:4 Predictive"
	case "EXTENDED":
		return "Extended"
	case "SVC":
		return "Scalable Video Coding"
	case "AVCHD":
		return "Advanced Video Codec High Definition"
	case "HEVC":
		return "High Efficiency Video Coding"
	default:
		return strings.TrimSpace(value)
	}
}

func canonicalEpisodeDetail(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "pilot":
		return "Pilot"
	case "final":
		return "Final"
	case "special":
		return "Special"
	case "unaired":
		return "Unaired"
	default:
		return ""
	}
}

func canonicalEpisodeFormat(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "Minisode") {
		return "Minisode"
	}
	return ""
}

func collectMetadataSegments(parts []string, base, parentTitle, sourceText string) []string {
	segments := make([]string, 0, len(parts)+8)
	for _, match := range bracketCaptureRE.FindAllStringSubmatch(sourceText, -1) {
		if len(match) > 1 {
			segments = append(segments, match[1])
		}
	}
	segments = append(segments, parts...)
	segments = append(segments, base, parentTitle)
	return uniqueNonEmpty(segments...)
}

func splitMetadataPiece(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '.', '_', '-', '/', '\\', '[', ']', '(', ')', '{', '}', '+', '&', ',':
			return true
		default:
			return false
		}
	})
	return uniqueNonEmpty(parts...)
}

func canonicalLanguageToken(value string) (string, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", false
	}
	if strings.HasPrefix(value, "vost") && len(value) > 4 {
		if language, ok := canonicalLanguageToken(value[4:]); ok {
			return language, true
		}
	}
	language, ok := languageAliases[value]
	return language, ok
}

func isSubtitleLikeLanguageToken(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "chs" || value == "cht" || strings.HasPrefix(value, "vost")
}

func containsAnyTerm(value string, terms []string) bool {
	tokens := splitMetadataPiece(value)
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term == "" {
			continue
		}
		if strings.Contains(term, " ") || containsHan(term) {
			if strings.Contains(strings.ToLower(value), term) {
				return true
			}
			continue
		}
		for _, token := range tokens {
			if strings.EqualFold(token, term) {
				return true
			}
		}
	}
	return false
}

func looksLikeEpisodeContinuationTitle(value string) bool {
	normalized := normalizeAliasText(value)
	return regexp.MustCompile(`^(E\d+( PART \d+)?|EPISODE \d+|PART \d+)$`).MatchString(normalized)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, current := range values {
		if strings.EqualFold(current, value) {
			return values
		}
	}
	return append(values, value)
}

func normalizeAliasText(value string) string {
	value = strings.ToUpper(value)
	value = strings.NewReplacer(".", " ", "_", " ", "-", " ", "/", " ", "\\", " ", "+", " + ", "&", " & ").Replace(value)
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func findAliasPosition(normalizedText, alias string) int {
	alias = normalizeAliasText(alias)
	if alias == "" {
		return -1
	}
	text := " " + normalizedText + " "
	needle := " " + alias + " "
	index := strings.Index(text, needle)
	if index >= 0 {
		return index - 1
	}
	if strings.Contains(alias, "+") {
		index = strings.Index(text, " "+strings.ReplaceAll(alias, " + ", "+")+" ")
		if index >= 0 {
			return index - 1
		}
	}
	return -1
}

func findPositionInsensitive(haystack, needle string) int {
	haystack = strings.ToLower(haystack)
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return -1
	}
	return strings.Index(haystack, needle)
}

func normalizeCompactValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "mbps", "Mbps")
	value = strings.ReplaceAll(value, "kbps", "Kbps")
	value = strings.ReplaceAll(value, "gb", "GB")
	value = strings.ReplaceAll(value, "mb", "MB")
	value = strings.ReplaceAll(value, "tb", "TB")
	return value
}
