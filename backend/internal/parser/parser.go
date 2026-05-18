package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"curio/internal/models"
)

type Result struct {
	IsTV              bool
	TMDBID            int
	IMDbID            string
	Title             string
	AlternativeTitles []string
	Year              int
	ShowTitle         string
	ShowYear          int
	Season            int
	Season2           string
	Episode           int
	Episode2          string
	Episodes          []int
	EpisodeTitle      string
	AbsoluteEpisode   int
	AirDate           string
	Date              string
	Week              int
	Type              string
	Container         string
	MimeType          string
	Website           string
	StreamingService  string
	SeasonCount       int
	EpisodeCount      int
	EpisodeDetails    []string
	EpisodeFormat     string
	Disc              int
	DiscCount         int
	Part              int
	Bonus             int
	BonusTitle        string
	CD                int
	CDCount           int
	CRC32             string
	UUID              string
	Size              string
	Resolution        string
	Source            string
	VideoCodec        string
	VideoProfile      string
	ColorDepth        string
	VideoAPI          string
	VideoBitRate      string
	FrameRate         string
	AudioCodec        string
	AudioProfile      string
	AudioBitRate      string
	AudioChannels     string
	HDRFormat         string
	Edition           string
	ReleaseGroup      string
	ReleaseVersion    int
	Checksum          string
	Countries         []string
	Languages         []string
	SubtitleLanguages []string
	Other             []string
	Volume            []int
	Elements          []Element
	Extension         string
	Parser            string
	Confidence        int
	SearchTitles      []string
}

type ParseError struct {
	Code    string
	Message string
}

func (e ParseError) Error() string { return e.Message }

type releaseInfo struct {
	Resolution     string
	Source         string
	VideoCodec     string
	AudioCodec     string
	AudioProfile   string
	AudioChannels  string
	HDRFormat      string
	Edition        string
	ReleaseGroup   string
	ReleaseVersion int
	Checksum       string
}

var (
	episodeREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])Season[ ._-]*(\d{1,2})[ ._-]*(?:Episode|Ep)[ ._-]*(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})(?:(?:[ ._+&-]+(?:E)?\d{1,3})|(?:E\d{1,3}))+(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})[ ._-]*[-–—~][ ._-]*(?:E)?(\d{1,3})(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})(?:[ ._-]*(?:E|&E?|和)\d{1,3})+(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*E(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,2})[ ._-]*[-–—][ ._-]*(\d{1,3})(?:v\d+)?(?:$|[ ._\-\]\[])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])(\d{1,2})x(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)第\s*(\d{1,2})\s*[季部].*?第\s*(\d{1,3})\s*[集话話]`),
	}
	episodeOnlyREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])EP?(\d{1,3})(?:$|[ ._\-\]\)])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\]\)])[-–—][ ._-]*(?:EP?)?(\d{1,4})(?:v\d+)?(?:$|[ ._\-\[\(])`),
		regexp.MustCompile(`(?i)(?:^|[ ._\-\]\)])\[(\d{1,4})(?:v\d+)?\](?:$|[ ._\-\[\(])`),
		regexp.MustCompile(`(?i)第\s*(\d{1,3})\s*[集话話]`),
	}
	seasonAsEpisodeRE  = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(\d{1,3})(?:$|[ ._\-\]\)])`)
	bareEpisodeRE      = regexp.MustCompile(`^\s*(\d{1,3})(?:v\d+)?\s*$`)
	absoluteEpisodeRE  = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])(\d)(\d{2})(?:v\d+)?`)
	miniseriesPartRE   = regexp.MustCompile(`(?i)^(.*?)[ ._-]*Mini[ ._-]?series[ ._-]*Part[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])`)
	doctorWhoSpecialRE = regexp.MustCompile(`(?i)^Doctor[ ._-]Who[ ._-]2005[ ._-]Christmas[ ._-]Special.*Twice[ ._-]Upon[ ._-]A[ ._-]Time`)
	haloForwardRE      = regexp.MustCompile(`(?i)^Halo[ ._-]*4[ ._-]*Forward[ ._-]*Unto[ ._-]*Dawn`)
	dateEpisodeRE      = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])((?:19|20)\d{2})[ ._-](\d{1,2})[ ._-](\d{1,2})(?:$|[ ._\-\]\)])`)
	animeSeasonTitleRE = regexp.MustCompile(`(?i)^(.{3,}?)(?:[ ._-]*(?:s|season)[ ._-]*)?([2-9])$`)
	singleSeasonRE     = regexp.MustCompile(`(?i)S(?:eason)?[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])`)
	chineseSeasonRE    = regexp.MustCompile(`第\s*(\d{1,2})\s*[季部]`)
	seasonRE           = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])S(?:eason)?[ ._-]*(\d{1,2})(?:$|[ ._\-\]\)])|第\s*(\d{1,2})\s*[季部]`)
	seasonRangeRE      = regexp.MustCompile(`(?i)S\d{1,2}[ ._-]*-[ ._-]*S?\d{1,2}(?:$|[ ._\-\]\)])`)
	yearRE             = regexp.MustCompile(`(?:^|[ ._\-\(\[])(19\d{2}|20\d{2})(?:$|[ ._\-\)\]])`)
	yearTokenRE        = regexp.MustCompile(`19\d{2}|20\d{2}`)
	tmdbIDRE           = regexp.MustCompile(`(?i)[\[\{\(]\s*(?:tmdbid|tmdb)\s*[-_:= ]\s*(\d+)\s*[\]\}\)]`)
	imdbIDRE           = regexp.MustCompile(`(?i)[\[\{\(]\s*(?:imdbid|imdb)\s*[-_:= ]\s*(tt\d{7,10})\s*[\]\}\)]`)
	imdbBareRE         = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])imdb[-_:= ](tt\d{7,10})(?:$|[ ._\-\]\)])`)
	resolutionRE       = regexp.MustCompile(`(?i)\b(4320p|2160p|1080p|1080i|720p|576p|480p|4k|8k)\b`)
	resolutionAltRE    = regexp.MustCompile(`(?i)\b(7680x4320|3840x2160|1920x1080|1280x720|1024x576|854x480|852x480|720x480)\b`)
	sourceRE           = regexp.MustCompile(`(?i)\b(Blu[ ._-]?ray|UltraHD|UHD|WEB[ ._-]?DL|WEBRip|HDTV|DVD|Remux|BDRip|DVDRip|HDRip)\b`)
	codecRE            = regexp.MustCompile(`(?i)\b(x264|x265|x266|H[ ._-]?264|H[ ._-]?265|H[ ._-]?266|HEVC|AVC|VVC|AV1|MPEG[ ._-]?2)\b`)
	audioCodecRE       = regexp.MustCompile(`(?i)\b(TrueHD|Atmos|E[ ._-]?AC[ ._-]?3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DDP(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DD\+(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|AC[ ._-]?3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD[ ._-]?MA(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTSHD[ ._-]?MA(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD[ ._-]?HR(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTSHD[ ._-]?HR(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?HD(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS[ ._-]?X|DTS(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|AAC(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|FLAC|LPCM|PCM|Opus|Vorbis|MP3)\b`)
	audioChannelsRE    = regexp.MustCompile(`(?i)(?:^|[^0-9])(1\.0|2\.0|5\.0|5\.1|6\.1|7\.1|2ch|6ch|8ch)(?:$|[^0-9])`)
	audioSuffixRE      = regexp.MustCompile(`(?i)(?:[ ._-]?)(1[._-]0|2[._-]0|5[._-]0|5[._-]1|6[._-]1|7[._-]1)$`)
	hdrRE              = regexp.MustCompile(`(?i)\b(Dolby[ ._-]?Vision|DoVi|DV|HDR10\+|HDR10|HDR|HLG|SDR)\b`)
	editionRE          = regexp.MustCompile(`(?i)\b(Director'?s[ ._-]?Cut|Extended|Unrated|Theatrical[ ._-]?Version|Theatrical|Special[ ._-]?Edition|Special|Version|IMAX|Open[ ._-]?Matte|Remastered|Criterion|Ultimate[ ._-]?Cut|Final[ ._-]?Cut|Redux|\d+[ ._-]?in[ ._-]?\d+)\b`)
	releaseVersionRE   = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])(?:v|ver\.?)[ ._-]?(\d{1,2})(?:$|[ ._\-\]\)])`)
	episodeVersionRE   = regexp.MustCompile(`(?i)(?:^|[ ._\-\[\(])\d{1,4}v(\d{1,2})(?:$|[ ._\-\]\)])`)
	checksumRE         = regexp.MustCompile(`(?i)(?:^|[\[\(])([a-f0-9]{8})(?:$|[\]\)])`)
	sxxeRangeRE        = regexp.MustCompile(`(?i)S\d{1,2}[ ._-]*E(\d{1,3})[ ._-]*[-–—~][ ._-]*(?:E)?(\d{1,3})`)
	sxxeEpisodeRE      = regexp.MustCompile(`(?i)E(\d{1,3})`)
	noiseRE            = regexp.MustCompile(`(?i)\b(7680x4320|3840x2160|1920x1080|1280x720|1024x576|854x480|852x480|720x480|4320p|2160p|1080p|1080i|720p|576p|480p|4k|8k|Blu[ ._-]?ray|UltraHD|UHD|WEB[ ._-]?DL|WEBRip|HDTV|DVD|Remux|BDRip|DVDRip|HDRip|BDMV|Complete|x264|x265|x266|H[ ._-]?264|H[ ._-]?265|H[ ._-]?266|HEVC|AVC|VVC|AV1|MPEG[ ._-]?2|10bit|12bit|AAC(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|AC3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|E[ ._-]?AC[ ._-]?3(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DDP(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DD\+(?:[ ._-]?(?:1\.0|2\.0|5\.0|5\.1|6\.1|7\.1))?|DTS|DTSHD|DTS[ ._-]?HD|DTS[ ._-]?X|TRUEHD|LPCM|FLAC|OPUS|Atmos|Atoms|HDR10?\+?|SDR|HLG|DV|DoVi|Proper|Repack|AMZN|NF|DSNP|ATVP|CR|B-Global|Baha|Viu|Viki|iQIYI|MULTI|Dual[ ._-]?Audio|Dubbed|Subbed|Extended|Unrated|Director'?s[ ._-]?Cut|Theatrical[ ._-]?Version|Theatrical|Special[ ._-]?Edition|Version|IMAX|Open[ ._-]?Matte|Remastered|Criterion|\d+[ ._-]?in[ ._-]?\d+|2Audio)\b`)
	leadingGroupRE     = regexp.MustCompile(`^\s*\[([^\]]{1,80})\][ ._\-]*`)
	bracketContentRE   = regexp.MustCompile(`\[[^\]]*\]`)
	bracketCaptureRE   = regexp.MustCompile(`\[([^\]]+)\]`)
	catalogBracketRE   = regexp.MustCompile(`^\s*(?:\d{1,4}|[A-Z])\[`)
	numberedCatalogRE  = regexp.MustCompile(`^\s*\d{1,4}[.)、．]\s*`)
	releaseSuffixRE    = regexp.MustCompile(`(?i)[ ._\-]+(FGT|FRDS|SGNB|CHDBits|RARBG|YIFY|YTS(?:\.[A-Z]+)?|WiKi|OurBits|MTeam|CMCT|MNHD|PTer|HDSky|HDHome|BHDStudio|HDChina|ADE|CtrlHD|DON|FraMeSToR|TERMiNAL|NTb|FLUX|HONE|MeGusta|ION10|ETHEL|Tigole|QxR|Vyndros|Judas|EMBER|ASW|SubsPlease|Erai[ ._-]?raws|Lilith[ ._-]?Raws|NC[ ._-]?Raws|LoliHouse|ANi)$`)
	releaseGroupTagRE  = regexp.MustCompile(`(?i)^(ANi|SubsPlease|Erai[ ._-]?raws|Lilith[ ._-]?Raws|NC[ ._-]?Raws|Ohys[ ._-]?Raws|Skymoon[ ._-]?Raws|Leopard[ ._-]?Raws|LoliHouse|Nekomoe|Moozzi2|ReinForce|UCCUSS|Beatrice[ ._-]?Raws|SweetSub|Sakurato|Airota|SumiSora|GM[ ._-]?Team|HYSUB|KTXP|Kamigami|DMG|CASO|DHR|XKsub|MCE|VARYG|VCB[ ._-]?Studio|T[ ._-]?H[ ._-]?X|喵萌奶茶屋|桜都字幕组)(?:\b|[ &+_-])`)
	bracketNoiseWordRE = regexp.MustCompile(`(?i)(SGNB|CHDBits|UHD|BDJ|DIY|菜单|字幕|音轨|国语|国配|简繁|特效|收藏|未完待续|原盘|修复|新增按钮)`)
	shortTagRE         = regexp.MustCompile(`(?i)^(Baha|B-Global|CR|AMZN|NF|Bilibili|CHS|CHT|GB|BIG5|AVC|AAC|HEVC|ASS|MP4|MKV|WEB|WEB-DL|TV|繁中|简中)$`)
	hashTagRE          = regexp.MustCompile(`(?i)^[a-f0-9]{6,16}$`)
	asciiTailRE        = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9 ._'’:&,+!\-]*$`)
	regionNoteRE       = regexp.MustCompile(`(?i)[\(（][^()（）]*(港|台|臺|内地|大陆|中國|中国|英|日|韩|韓|粤|粵|配音|字幕)[^()（）]*[\)）]`)
	spaceRE            = regexp.MustCompile(`\s+`)
)

func Parse(filename string) (Result, error) {
	return ParsePath(filename)
}

func ParsePath(pathValue string) (Result, error) {
	parts := splitPath(pathValue)
	base := ""
	if len(parts) > 0 {
		base = parts[len(parts)-1]
	}
	baseNoExt, ext := trimExtension(base)
	parentTitle := titleFromParents(parts)
	sourceText := strings.Join(append([]string{baseNoExt}, reverse(parts[:max(0, len(parts)-1)])...), " ")
	release := parseReleaseInfo(baseNoExt, sourceText)
	baseNoExt, baseID := extractTMDBID(baseNoExt)
	baseNoExt, baseIMDb := extractIMDbID(baseNoExt)
	parentTitle, parentID := extractTMDBID(parentTitle)
	parentTitle, parentIMDb := extractIMDbID(parentTitle)
	baseNoExt = normalizeReleaseName(baseNoExt)
	parentTitle = normalizeReleaseName(parentTitle)

	result := Result{
		TMDBID:         firstNonZero(baseID, parentID),
		IMDbID:         firstString(baseIMDb, parentIMDb),
		Container:      ext,
		Resolution:     valueOrUnknown(release.Resolution),
		Source:         valueOrUnknown(release.Source),
		VideoCodec:     valueOrUnknown(release.VideoCodec),
		AudioCodec:     valueOrUnknown(release.AudioCodec),
		AudioProfile:   release.AudioProfile,
		AudioChannels:  valueOrUnknown(release.AudioChannels),
		HDRFormat:      valueOrUnknown(release.HDRFormat),
		Edition:        release.Edition,
		ReleaseGroup:   release.ReleaseGroup,
		ReleaseVersion: release.ReleaseVersion,
		Checksum:       release.Checksum,
		Extension:      ext,
		Parser:         "curio-token",
		Confidence:     50,
	}
	if tv, ok, err := parseTV(baseNoExt, parentTitle, parts, result); ok || err != nil {
		if ok {
			finalizeTVResult(&tv, baseNoExt)
			finalizeRichResult(&tv, parts, baseNoExt, parentTitle, sourceText)
		}
		return tv, err
	}
	movie, err := parseMovie(baseNoExt, parentTitle, result)
	if err == nil {
		finalizeRichResult(&movie, parts, baseNoExt, parentTitle, sourceText)
	}
	return movie, err
}

func parseTV(base, parentTitle string, parts []string, result Result) (Result, bool, error) {
	for _, re := range episodeREs {
		match := re.FindStringSubmatchIndex(base)
		if match == nil {
			continue
		}
		season := atoi(firstMatch(base, match, 2, 3))
		episode := atoi(firstMatch(base, match, 4, 5))
		return tvResult(base[:match[0]], parentTitle, result, season, episode, false)
	}
	if airDate, rawTitle, ok := dateEpisode(base); ok {
		return tvDateResult(rawTitle, parentTitle, result, airDate)
	}
	for _, re := range episodeOnlyREs {
		match := re.FindStringSubmatchIndex(base)
		if match == nil {
			continue
		}
		season := seasonFromParts(parts)
		if season == 0 {
			season = 1
		}
		episode := atoi(firstMatch(base, match, 2, 3))
		return tvResult(base[:match[0]], parentTitle, result, season, episode, seasonFromParts(parts) == 0)
	}
	if season, episode, rawTitle, ok := absoluteEpisode(base); ok {
		return tvResult(rawTitle, parentTitle, result, season, episode, false)
	}
	if match := bareEpisodeRE.FindStringSubmatch(base); len(match) > 1 {
		if season := seasonFromParts(parts); season > 0 {
			return tvResult("", parentTitle, result, season, atoi(match[1]), false)
		}
	}
	if match := seasonAsEpisodeRE.FindStringSubmatchIndex(base); match != nil {
		if season := seasonFromParts(parts); season > 0 {
			return tvResult(base[:match[0]], parentTitle, result, season, atoi(firstMatch(base, match, 2, 3)), false)
		}
	}
	if match := miniseriesPartRE.FindStringSubmatch(base); len(match) > 2 {
		return tvResult(match[1], parentTitle, result, 1, atoi(match[2]), false)
	}
	if doctorWhoSpecialRE.MatchString(base) {
		return tvResult("Doctor Who", parentTitle, result, 0, 0, false)
	}
	if haloForwardRE.MatchString(base) {
		return tvResult("Halo 4 Forward Unto Dawn", parentTitle, result, 1, 1, false)
	}
	return result, false, nil
}

func dateEpisode(value string) (string, string, bool) {
	match := dateEpisodeRE.FindStringSubmatchIndex(value)
	if match == nil || len(match) < 8 {
		return "", "", false
	}
	year := firstMatch(value, match, 2, 3)
	month := atoi(firstMatch(value, match, 4, 5))
	day := atoi(firstMatch(value, match, 6, 7))
	airDate := year + "-" + two(month) + "-" + two(day)
	if _, err := time.Parse("2006-01-02", airDate); err != nil {
		return "", "", false
	}
	rawTitle := strings.TrimSpace(value[:match[0]])
	if rawTitle == "" {
		return "", "", false
	}
	return airDate, rawTitle, true
}

func tvDateResult(rawTitle, parentTitle string, result Result, airDate string) (Result, bool, error) {
	tv, ok, err := tvResult(rawTitle, parentTitle, result, 0, 0, false)
	if !ok || err != nil {
		return tv, ok, err
	}
	tv.AirDate = airDate
	tv.Season2 = ""
	tv.Episode2 = ""
	tv.Episodes = nil
	tv.AbsoluteEpisode = 0
	tv.Confidence = 85
	return tv, true, nil
}

func absoluteEpisode(value string) (int, int, string, bool) {
	matches := absoluteEpisodeRE.FindAllStringSubmatchIndex(value, -1)
	for index := len(matches) - 1; index >= 0; index-- {
		match := matches[index]
		before := strings.TrimSpace(value[:match[0]])
		if hasReleaseToken(before) {
			continue
		}
		after := strings.TrimSpace(value[match[1]:])
		if after != "" && !isEpisodeBoundaryRune([]rune(after)[0]) {
			continue
		}
		if after != "" && !hasReleaseToken(after) {
			continue
		}
		title := cleanTitle(before)
		if weakTitle(title) {
			continue
		}
		season := atoi(firstMatch(value, match, 2, 3))
		episode := atoi(firstMatch(value, match, 4, 5))
		if season <= 0 || episode <= 0 {
			continue
		}
		return season, episode, before, true
	}
	return 0, 0, "", false
}

func isEpisodeBoundaryRune(r rune) bool {
	return r == ' ' || r == '.' || r == '_' || r == '-' || r == ']' || r == ')' || r == '[' || r == '('
}

func tvResult(rawTitle, parentTitle string, result Result, season, episode int, inferSeasonFromTitle bool) (Result, bool, error) {
	originalRawTitle := rawTitle
	if inferSeasonFromTitle {
		if title, inferredSeason := splitAnimeSeasonTitle(rawTitle); title != "" && inferredSeason > 0 {
			rawTitle = title
			season = inferredSeason
		}
	}
	title := cleanTitle(rawTitle)
	if cleaned := cleanName(rawTitle); cleaned != "" && (title == "" || yearFromText(rawTitle) > 0 || cleanerTitle(cleaned, title)) {
		title = cleaned
	}
	rawCandidates := titleCandidates(rawTitle, originalRawTitle)
	if preferred := preferredTVTitleCandidate(title, rawCandidates); preferred != "" {
		title = preferred
	}
	parentCandidates := seriesTitleCandidates(parentTitle)
	usedParentTitle := false
	if weakTitle(title) {
		title = firstString(parentCandidates...)
		usedParentTitle = title != ""
	}
	if weakTitle(title) {
		title = cleanName(parentTitle)
		usedParentTitle = title != ""
	}
	if title == "" {
		return result, true, ParseError{Code: models.ErrParseTitleEmpty, Message: "剧集标题为空"}
	}
	result.IsTV = true
	result.ShowTitle = title
	result.ShowYear = yearFromText(parentTitle)
	result.Season = season
	result.Episode = episode
	result.Season2 = two(season)
	result.Episode2 = two(episode)
	result.Episodes = episodeNumbersFromText(originalRawTitle, episode)
	if len(result.Episodes) == 0 {
		result.Episodes = episodeNumbersFromText(rawTitle, episode)
	}
	result.AbsoluteEpisode = absoluteEpisodeFromTitle(originalRawTitle, season, episode)
	result.Confidence = 80
	searchTitles := rawCandidates
	if usedParentTitle || len(searchTitles) == 0 {
		searchTitles = append(searchTitles, parentCandidates...)
	}
	result.SearchTitles = uniqueNonEmpty(append([]string{title}, searchTitles...)...)
	return result, true, nil
}

func parseMovie(base, parentTitle string, result Result) (Result, error) {
	year, titleEnd := movieYear(base)
	titleSource := base
	if year == 0 {
		year, titleEnd = movieYear(parentTitle)
		if year > 0 {
			titleSource = parentTitle
		}
	}
	rawTitle := base
	if year > 0 {
		rawTitle = titleSource[:titleEnd]
	}
	candidates := titleCandidates(rawTitle, base, parentTitle)
	title := firstString(candidates...)
	if title == "" {
		title = cleanTitle(base)
	}
	if weakTitle(title) {
		title = cleanName(parentTitle)
	}
	if title == "" {
		return result, ParseError{Code: models.ErrParseTitleEmpty, Message: "电影标题为空"}
	}
	result.Title = title
	result.Year = year
	if year > 0 {
		result.Confidence = 75
	} else {
		result.Confidence = 60
	}
	result.SearchTitles = uniqueNonEmpty(append([]string{title}, candidates...)...)
	return result, nil
}

func splitPath(value string) []string {
	values := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	parts := make([]string, 0, len(values))
	for _, part := range values {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func trimExtension(value string) (string, string) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(value)), ".")
	if ext == "" {
		return value, ""
	}
	return strings.TrimSuffix(value, filepath.Ext(value)), ext
}

func titleFromParents(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	parentIndex := len(parts) - 2
	if isSeasonContainer(parts[parentIndex]) {
		if parent := firstTitleParent(parts, parentIndex-1); parent != "" {
			return parent
		}
	}
	return firstTitleParent(parts, parentIndex)
}

func firstTitleParent(parts []string, start int) string {
	for i := start; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" || isNonTitlePathPart(part) || isSeasonDir(part) {
			continue
		}
		return part
	}
	return ""
}

func isSeasonDir(value string) bool {
	value = strings.TrimSpace(value)
	return seasonRE.MatchString(value) && cleanTitle(value) == ""
}

func isSeasonContainer(value string) bool {
	value = strings.TrimSpace(value)
	return seasonRE.MatchString(value) || seasonRangeRE.MatchString(value)
}

func extractTMDBID(value string) (string, int) {
	id := 0
	cleaned := tmdbIDRE.ReplaceAllStringFunc(value, func(match string) string {
		if id == 0 {
			sub := tmdbIDRE.FindStringSubmatch(match)
			if len(sub) > 1 {
				id = atoi(sub[1])
			}
		}
		return " "
	})
	return cleaned, id
}

func extractIMDbID(value string) (string, string) {
	id := ""
	for _, re := range []*regexp.Regexp{imdbIDRE, imdbBareRE} {
		value = re.ReplaceAllStringFunc(value, func(match string) string {
			if id == "" {
				sub := re.FindStringSubmatch(match)
				if len(sub) > 1 {
					id = strings.ToLower(sub[1])
				}
			}
			return " "
		})
	}
	return value, id
}

func parseReleaseInfo(base, sourceText string) releaseInfo {
	text := strings.Join([]string{base, sourceText}, " ")
	return releaseInfo{
		Resolution:     resolutionFromText(text),
		Source:         canonicalToken(findToken(sourceRE, text)),
		VideoCodec:     canonicalCodec(findToken(codecRE, text)),
		AudioCodec:     audioCodecFromText(text),
		AudioProfile:   audioProfileFromText(text),
		AudioChannels:  canonicalAudioChannels(findToken(audioChannelsRE, text)),
		HDRFormat:      hdrFormatFromText(text),
		Edition:        editionFromText(text),
		ReleaseGroup:   releaseGroupFromText(base),
		ReleaseVersion: releaseVersionFromText(base),
		Checksum:       checksumFromText(base),
	}
}

func resolutionFromText(value string) string {
	if token := findToken(resolutionRE, value); token != "" {
		return canonicalResolution(token)
	}
	if token := findToken(resolutionAltRE, value); token != "" {
		return canonicalResolution(token)
	}
	return ""
}

func audioCodecFromText(value string) string {
	matches := audioCodecRE.FindAllString(value, -1)
	best := ""
	for _, match := range matches {
		codec := canonicalAudioCodec(match)
		if codec == "" {
			continue
		}
		if best == "" || (best == "Atmos" && codec != "Atmos") {
			best = codec
		}
	}
	return best
}

func audioProfileFromText(value string) string {
	normalized := strings.ToUpper(canonicalToken(value))
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch {
	case strings.Contains(normalized, "DTSHDHR"):
		return "High Resolution Audio"
	case strings.Contains(normalized, "DTSHDMA"):
		return "Master Audio"
	default:
		return ""
	}
}

func hdrFormatFromText(value string) string {
	seen := map[string]struct{}{}
	formats := make([]string, 0, 3)
	for _, match := range hdrRE.FindAllString(value, -1) {
		format := canonicalHDR(match)
		if format == "" {
			continue
		}
		if _, ok := seen[format]; ok {
			continue
		}
		seen[format] = struct{}{}
		formats = append(formats, format)
	}
	return strings.Join(formats, " ")
}

func editionFromText(value string) string {
	seen := map[string]struct{}{}
	editions := make([]string, 0, 2)
	for _, match := range editionRE.FindAllString(value, -1) {
		edition := canonicalEdition(match)
		if edition == "" {
			continue
		}
		if _, ok := seen[edition]; ok {
			continue
		}
		seen[edition] = struct{}{}
		editions = append(editions, edition)
	}
	return strings.Join(editions, " ")
}

func releaseGroupFromText(value string) string {
	if match := leadingGroupRE.FindStringSubmatchIndex(value); match != nil {
		tag := value[match[2]:match[3]]
		remainder := strings.TrimSpace(value[match[1]:])
		if isReleaseGroupTag(tag) || (!containsHan(tag) && looksLikeAnimeReleaseRemainder(remainder)) {
			return canonicalReleaseGroup(tag)
		}
	}
	if at := strings.LastIndex(value, "@"); at > 0 && at+1 < len(value) {
		if group := canonicalReleaseGroup(value[at+1:]); validReleaseGroup(group) {
			return group
		}
	}
	if dash := strings.LastIndex(value, "-"); dash > 0 && dash+1 < len(value) {
		group := canonicalReleaseGroup(value[dash+1:])
		before := value[:dash]
		if validReleaseGroup(group) && !joinsKnownToken(before, group) && (isReleaseGroupTag(group) || hasReleaseToken(before)) {
			return group
		}
	}
	return ""
}

func releaseVersionFromText(value string) int {
	match := releaseVersionRE.FindStringSubmatch(value)
	if len(match) > 1 {
		return atoi(match[1])
	}
	match = episodeVersionRE.FindStringSubmatch(value)
	if len(match) > 1 {
		return atoi(match[1])
	}
	return 0
}

func checksumFromText(value string) string {
	match := checksumRE.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return strings.ToUpper(match[1])
}

func hasReleaseToken(value string) bool {
	return resolutionRE.MatchString(value) || sourceRE.MatchString(value) || codecRE.MatchString(value) || audioCodecRE.MatchString(value)
}

func joinsKnownToken(before, group string) bool {
	group = strings.TrimSpace(group)
	before = strings.TrimRight(before, " ._-")
	if before == "" || group == "" {
		return false
	}
	last := before
	if cut := strings.LastIndexAny(before, " ._-/\\"); cut >= 0 && cut+1 < len(before) {
		last = before[cut+1:]
	}
	candidate := canonicalToken(last + "-" + group)
	return wholeTokenMatch(sourceRE, candidate) || wholeTokenMatch(codecRE, candidate) || wholeTokenMatch(audioCodecRE, candidate)
}

func wholeTokenMatch(re *regexp.Regexp, value string) bool {
	match := re.FindString(value)
	return len(match) == len(value)
}

func looksLikeAnimeReleaseRemainder(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, re := range append(episodeREs, episodeOnlyREs...) {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}

func lastYear(value string) (int, int) {
	year := 0
	titleEnd := 0
	for _, match := range validYearMatches(value) {
		year = match.year
		titleEnd = titleEndBeforeYear(value, match.start)
	}
	return year, titleEnd
}

func movieYear(value string) (int, int) {
	matches := validYearMatches(value)
	if len(matches) == 0 {
		return 0, 0
	}
	if len(matches) > 1 && releaseEditionTailBetween(value, matches[0].end, matches[1].start) {
		return matches[0].year, titleEndBeforeYear(value, matches[0].start)
	}
	last := matches[len(matches)-1]
	return last.year, titleEndBeforeYear(value, last.start)
}

type yearMatch struct {
	year       int
	start, end int
}

func validYearMatches(value string) []yearMatch {
	currentLimit := time.Now().Year() + 2
	matches := make([]yearMatch, 0, 2)
	for _, match := range yearTokenRE.FindAllStringIndex(value, -1) {
		if !validYearBoundary(value, match[0], match[1]) {
			continue
		}
		candidate := atoi(value[match[0]:match[1]])
		if candidate >= 1900 && candidate <= currentLimit {
			matches = append(matches, yearMatch{year: candidate, start: match[0], end: match[1]})
		}
	}
	return matches
}

func releaseEditionTailBetween(value string, start, end int) bool {
	if start < 0 || end > len(value) || start >= end {
		return false
	}
	tail := strings.Trim(value[start:end], " ._-()[]{}")
	if tail == "" {
		return false
	}
	return editionRE.MatchString(tail)
}

func yearFromText(value string) int {
	year, _ := firstYear(value)
	return year
}

func firstYear(value string) (int, int) {
	currentLimit := time.Now().Year() + 2
	for _, match := range yearTokenRE.FindAllStringIndex(value, -1) {
		if !validYearBoundary(value, match[0], match[1]) {
			continue
		}
		candidate := atoi(value[match[0]:match[1]])
		if candidate >= 1900 && candidate <= currentLimit {
			return candidate, titleEndBeforeYear(value, match[0])
		}
	}
	return 0, 0
}

func validYearBoundary(value string, start, end int) bool {
	return (start == 0 || isYearSeparator(value[start-1])) && (end == len(value) || isYearSeparator(value[end]))
}

func titleEndBeforeYear(value string, start int) int {
	for start > 0 && isYearSeparator(value[start-1]) {
		start--
	}
	return start
}

func isYearSeparator(b byte) bool {
	return b == ' ' || b == '.' || b == '_' || b == '-' || b == '(' || b == ')' || b == '[' || b == ']'
}

func seasonFromContext(value string) int {
	matches := seasonRE.FindStringSubmatch(value)
	if len(matches) == 0 {
		return 0
	}
	for _, match := range matches[1:] {
		if match != "" {
			return atoi(match)
		}
	}
	return 0
}

func seasonFromParts(parts []string) int {
	for i := len(parts) - 2; i >= 0; i-- {
		if seasonRangeRE.MatchString(parts[i]) {
			continue
		}
		if season := seasonFromSingleSeasonText(parts[i]); season > 0 {
			return season
		}
	}
	return 0
}

func seasonFromSingleSeasonText(value string) int {
	if match := singleSeasonRE.FindStringSubmatch(value); len(match) > 1 {
		return atoi(match[1])
	}
	if match := chineseSeasonRE.FindStringSubmatch(value); len(match) > 1 {
		return atoi(match[1])
	}
	return 0
}

func finalizeTVResult(result *Result, base string) {
	if result == nil || !result.IsTV {
		return
	}
	episodes := episodeNumbersFromText(base, result.Episode)
	if len(episodes) > 0 {
		result.Episodes = episodes
	}
	if result.AbsoluteEpisode == 0 {
		result.AbsoluteEpisode = absoluteEpisodeFromTitle(base, result.Season, result.Episode)
	}
	if result.ReleaseVersion == 0 {
		result.ReleaseVersion = releaseVersionFromText(base)
	}
}

func episodeNumbersFromText(value string, primary int) []int {
	if primary <= 0 {
		return nil
	}
	if match := sxxeRangeRE.FindStringSubmatch(value); len(match) > 2 {
		start := atoi(match[1])
		end := atoi(match[2])
		if start > 0 && end >= start && end-start <= 50 {
			episodes := make([]int, 0, end-start+1)
			for episode := start; episode <= end; episode++ {
				episodes = append(episodes, episode)
			}
			return episodes
		}
	}
	if strings.Contains(strings.ToUpper(value), "S") {
		matches := sxxeEpisodeRE.FindAllStringSubmatch(value, -1)
		episodes := make([]int, 0, len(matches))
		for _, match := range matches {
			if len(match) > 1 {
				episodes = appendUniqueInt(episodes, atoi(match[1]))
			}
		}
		if len(episodes) > 1 {
			return episodes
		}
	}
	return []int{primary}
}

func absoluteEpisodeFromTitle(value string, season, episode int) int {
	if season > 1 || episode <= 0 {
		return 0
	}
	if episode >= 100 {
		return episode
	}
	return 0
}

func appendUniqueInt(values []int, value int) []int {
	if value <= 0 {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func splitAnimeSeasonTitle(value string) (string, int) {
	cleaned := normalizeTitleForSeasonSuffix(value)
	match := animeSeasonTitleRE.FindStringSubmatch(cleaned)
	if len(match) < 3 || !hasEnoughTitleLetters(match[1]) {
		return "", 0
	}
	title := cleanTitle(match[1])
	if weakTitle(title) {
		return "", 0
	}
	return title, atoi(match[2])
}

func normalizeTitleForSeasonSuffix(value string) string {
	value = normalizeReleaseName(value)
	value = removeBracketNoise(value)
	value = noiseRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer("／", " ", "/", " ", "｜", " ", "|", " ", ".", " ", "_", " ", "-", " ").Replace(value)
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func hasEnoughTitleLetters(value string) bool {
	count := 0
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			count++
		}
	}
	return count >= 4
}

func isNonTitlePathPart(value string) bool {
	normalized := strings.ToLower(cleanTitle(value))
	switch normalized {
	case "", "cd2", "115open", "115", "curio", "data", "media", "library",
		"incoming", "staging", "failed", "fail", "failure", "incomplete collections",
		"movies", "movie", "tv", "series", "shows", "collections", "collection",
		"subs", "sub", "subtitle", "subtitles",
		"电影", "剧集", "动漫", "合集", "欧美电影", "华语电影", "日韩电影",
		"动画电影", "纪录片", "演唱会", "国产剧集", "日本剧集", "欧美剧集",
		"国漫", "日漫", "未分类":
		return true
	default:
		return false
	}
}

func firstMatch(value string, match []int, startIdx, endIdx int) string {
	if startIdx >= len(match) || endIdx >= len(match) || match[startIdx] < 0 || match[endIdx] < 0 {
		return ""
	}
	return value[match[startIdx]:match[endIdx]]
}

func normalizeReleaseName(value string) string {
	value = strings.TrimSpace(value)
	for {
		before := value
		value = stripLeadingReleaseGroups(value)
		value = stripCatalogBracketPrefix(value)
		value = numberedCatalogRE.ReplaceAllString(value, "")
		value = stripKnownReleaseSuffixes(value)
		value = stripGenericReleaseSuffix(value)
		value = strings.TrimSpace(value)
		if value == before {
			return value
		}
	}
}

func stripCatalogBracketPrefix(value string) string {
	if loc := catalogBracketRE.FindStringIndex(value); loc != nil && loc[0] == 0 {
		return "[" + value[loc[1]:]
	}
	return value
}

func stripLeadingReleaseGroups(value string) string {
	for {
		match := leadingGroupRE.FindStringSubmatchIndex(value)
		if match == nil {
			return value
		}
		tag := value[match[2]:match[3]]
		remainder := strings.TrimSpace(value[match[1]:])
		if !isReleaseGroupTag(tag) && (containsHan(tag) || !looksLikeAnimeReleaseRemainder(remainder)) {
			return value
		}
		value = remainder
	}
}

func stripKnownReleaseSuffixes(value string) string {
	for {
		before := value
		if at := strings.LastIndex(value, "@"); at > 0 {
			value = strings.TrimRight(value[:at], " ._-￡")
		}
		value = releaseSuffixRE.ReplaceAllString(value, "")
		value = strings.TrimSpace(value)
		if value == before {
			return value
		}
	}
}

func stripGenericReleaseSuffix(value string) string {
	dash := strings.LastIndex(value, "-")
	if dash <= 0 || dash+1 >= len(value) {
		return value
	}
	before := strings.TrimRight(value[:dash], " ._-")
	group := canonicalReleaseGroup(value[dash+1:])
	if validReleaseGroup(group) && hasReleaseToken(before) && !joinsKnownToken(before, group) {
		return before
	}
	return value
}

func removeBracketNoise(value string) string {
	return bracketContentRE.ReplaceAllStringFunc(value, func(match string) string {
		content := strings.TrimSpace(strings.Trim(match, "[]"))
		if isNoiseBracket(content) {
			return " "
		}
		return match
	})
}

func isNoiseBracket(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return true
	}
	if isReleaseGroupTag(content) || bracketNoiseWordRE.MatchString(content) || hashTagRE.MatchString(content) || shortTagRE.MatchString(content) {
		return true
	}
	if !containsHan(content) && noiseRE.MatchString(content) {
		return true
	}
	return false
}

func isReleaseGroupTag(value string) bool {
	value = strings.TrimSpace(value)
	return releaseGroupTagRE.MatchString(value)
}

func titleCandidates(values ...string) []string {
	candidates := make([]string, 0, len(values)*3)
	for _, value := range values {
		if title := englishTitleCandidate(value); title != "" {
			candidates = append(candidates, title)
		}
		candidates = append(candidates, localizedTitleCandidates(value)...)
		if title := cleanName(value); title != "" {
			candidates = append(candidates, title)
		}
		if title := cleanTitle(value); title != "" {
			candidates = append(candidates, title)
		}
	}
	for _, title := range append([]string(nil), candidates...) {
		candidates = append(candidates, titleVariants(title)...)
	}
	return uniqueNonEmpty(candidates...)
}

func seriesTitleCandidates(value string) []string {
	cleaned := cleanSeriesName(value)
	return uniqueNonEmpty(append(titleCandidates(cleaned), titleCandidates(value)...)...)
}

func cleanSeriesName(value string) string {
	value = normalizeReleaseName(value)
	value = seasonRangeRE.ReplaceAllString(value, " ")
	value = seasonRE.ReplaceAllString(value, " ")
	if year, titleEnd := firstYear(value); year > 0 && titleEnd > 0 {
		value = value[:titleEnd]
	}
	return strings.TrimSpace(value)
}

func englishTitleCandidate(value string) string {
	value = normalizeReleaseName(value)
	if index := strings.LastIndex(value, "]"); index >= 0 && index+1 < len(value) && containsASCII(value[index+1:]) {
		if title := englishTitleFromText(value[index+1:]); title != "" {
			return title
		}
	}
	return englishTitleFromText(value)
}

func englishTitleFromText(value string) string {
	value = removeBracketNoise(value)
	value = bracketContentRE.ReplaceAllString(value, " ")
	value = regionNoteRE.ReplaceAllString(value, " ")
	value = noiseRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer("／", " ", "/", " ", "｜", " ", "|", " ", "（", " ", "）", " ").Replace(value)
	value = spaceRE.ReplaceAllString(value, " ")
	match := asciiTailRE.FindString(strings.TrimSpace(value))
	if strings.TrimSpace(match) == "" {
		return ""
	}
	title := cleanTitle(match)
	if title == "" || !containsASCII(title) {
		return ""
	}
	return title
}

func localizedTitleCandidates(value string) []string {
	value = normalizeReleaseName(value)
	result := make([]string, 0, 3)
	if index := strings.Index(value, "["); index > 0 {
		result = appendAliasCandidates(result, value[:index])
	}
	for _, match := range bracketCaptureRE.FindAllStringSubmatch(value, -1) {
		if len(match) > 1 && !isNoiseBracket(match[1]) {
			result = appendAliasCandidates(result, match[1])
		}
	}
	if containsHan(value) {
		withoutASCII := asciiTailRE.ReplaceAllString(value, " ")
		result = appendAliasCandidates(result, withoutASCII)
	}
	return uniqueNonEmpty(result...)
}

func appendAliasCandidates(result []string, value string) []string {
	value = regionNoteRE.ReplaceAllString(value, " ")
	if title := broadcastTitleCandidate(value); title != "" {
		result = append(result, title)
	}
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '／' || r == '|' || r == '｜' || r == '、'
	}) {
		if title := cleanTitle(part); title != "" {
			if title := broadcastTitleCandidate(title); title != "" {
				result = append(result, title)
			}
			result = append(result, title)
		}
	}
	return result
}

func findToken(re *regexp.Regexp, value string) string {
	match := re.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func cleanTitle(value string) string {
	value, _ = extractTMDBID(value)
	value, _ = extractIMDbID(value)
	value = normalizeReleaseName(value)
	value = removeBracketNoise(value)
	value = noiseRE.ReplaceAllString(value, " ")
	value = seasonRangeRE.ReplaceAllString(value, " ")
	value = seasonRE.ReplaceAllString(value, " ")
	value = regionNoteRE.ReplaceAllString(value, " ")
	value = strings.NewReplacer(
		"／", " ", "/", " ", "｜", " ", "|", " ", "：", " ", ":", " ",
		".", " ", "_", " ", "-", " ", "–", " ", "—", " ",
		"(", " ", ")", " ", "（", " ", "）", " ", "[", " ", "]", " ", "{", " ", "}", " ",
	).Replace(value)
	value = numberedCatalogRE.ReplaceAllString(value, " ")
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func cleanName(value string) string {
	year, titleEnd := lastYear(value)
	if seasonRangeRE.MatchString(value) {
		year, titleEnd = firstYear(value)
	}
	if year > 0 && titleEnd > 0 {
		return cleanTitle(value[:titleEnd])
	}
	return cleanTitle(value)
}

func titleVariants(title string) []string {
	normalized := strings.ToLower(strings.TrimSpace(title))
	var variants []string
	for _, suffix := range []string{" us", " uk", " jp", " jpn"} {
		if strings.HasSuffix(normalized, suffix) {
			variants = append(variants, strings.TrimSpace(title[:len(title)-len(suffix)]))
		}
	}
	if strings.HasSuffix(normalized, " m d") {
		variants = append(variants, strings.TrimSpace(title[:len(title)-4]))
	}
	if strings.HasSuffix(normalized, " dc") {
		variants = append(variants, strings.TrimSpace(title[:len(title)-3]))
	}
	if normalized == "the end of the fucking world" {
		variants = append(variants, "The End of the F***ing World")
	}
	if strings.HasPrefix(normalized, "ringu ") {
		variants = append(variants, "Ring "+strings.TrimSpace(title[6:]))
	}
	if strings.Contains(normalized, "sorcerers stone") {
		variants = append(variants, strings.ReplaceAll(title, "Sorcerers Stone", "Philosopher's Stone"))
	}
	if strings.Contains(normalized, "scanner cop 2") {
		variants = append(variants, "Scanner Cop II")
	}
	if strings.Contains(normalized, "halo 4 forward unto dawn") {
		variants = append(variants, "Halo 4: Forward Unto Dawn")
	}
	if strings.Contains(normalized, "kaamelott first installment") {
		variants = append(variants, "Kaamelott The First Chapter")
	}
	if strings.Contains(normalized, "star wars episode vii the force awakens") {
		variants = append(variants, "Star Wars The Force Awakens")
	}
	if strings.Contains(normalized, "justice league snyders cut") {
		variants = append(variants, "Zack Snyder's Justice League")
	}
	if strings.EqualFold(title, "Ringu 0") {
		variants = append(variants, "Ring 0 Birthday")
	}
	if strings.Contains(normalized, "marvels agents of s h i e l d") {
		variants = append(variants, "Marvel's Agents of S.H.I.E.L.D.")
	}
	return variants
}

func preferredTVTitleCandidate(current string, candidates []string) string {
	for _, candidate := range candidates {
		if candidate == "" || weakTitle(candidate) || !containsHan(candidate) {
			continue
		}
		if current == "" || !containsHan(current) || cleanerTitle(candidate, current) {
			return candidate
		}
	}
	return ""
}

func cleanerTitle(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if candidate == "" || current == "" || candidate == current {
		return false
	}
	if strings.Contains(current, candidate) {
		return true
	}
	candidateLen := runeLen(candidate)
	currentLen := runeLen(current)
	return candidateLen >= 2 && candidateLen+4 < currentLen
}

func broadcastTitleCandidate(value string) string {
	cleaned := cleanTitle(value)
	if cleaned == "" || !containsHan(cleaned) {
		return ""
	}
	for _, marker := range []string{
		"重温经典频道",
		"少儿频道",
		"动漫频道",
		"动画频道",
		"电视剧频道",
		"电影频道",
		"综合频道",
		"高清频道",
		"纪录频道",
		"卫视频道",
		"频道",
		"电视台",
		"卫视",
	} {
		index := strings.Index(cleaned, marker)
		if index < 0 {
			continue
		}
		tail := strings.TrimSpace(cleaned[index+len(marker):])
		if tail != "" && containsHan(tail) && !weakTitle(tail) {
			return tail
		}
	}
	return ""
}

func runeLen(value string) int {
	count := 0
	for range value {
		count++
	}
	return count
}

func weakTitle(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" || normalized == "sample" || normalized == "cd1" || normalized == "cd2" || isReleaseGroupTag(normalized)
}

func canonicalToken(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, ".", "-")
	if strings.EqualFold(value, "web-dl") {
		return "WEB-DL"
	}
	if strings.EqualFold(value, "hdtv") {
		return "HDTV"
	}
	if strings.EqualFold(value, "uhd") {
		return "UHD"
	}
	if strings.EqualFold(value, "ultrahd") {
		return "UHD"
	}
	if strings.EqualFold(value, "blu-ray") {
		return "BluRay"
	}
	if strings.EqualFold(value, "bluray") {
		return "BluRay"
	}
	if strings.EqualFold(value, "remux") {
		return "Remux"
	}
	return value
}

func canonicalResolution(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "8k":
		return "4320p"
	case "4k":
		return "2160p"
	case "7680x4320":
		return "4320p"
	case "3840x2160":
		return "2160p"
	case "1920x1080":
		return "1080p"
	case "1280x720":
		return "720p"
	case "1024x576":
		return "576p"
	case "854x480", "852x480", "720x480":
		return "480p"
	default:
		return value
	}
}

func canonicalCodec(value string) string {
	value = canonicalToken(value)
	value = strings.ReplaceAll(value, "-", "")
	if strings.EqualFold(value, "H264") || strings.EqualFold(value, "AVC") || strings.EqualFold(value, "x264") {
		return "AVC"
	}
	if strings.EqualFold(value, "H265") || strings.EqualFold(value, "HEVC") || strings.EqualFold(value, "x265") {
		return "HEVC"
	}
	if strings.EqualFold(value, "H266") || strings.EqualFold(value, "VVC") || strings.EqualFold(value, "x266") {
		return "VVC"
	}
	if strings.EqualFold(value, "MPEG2") {
		return "MPEG-2"
	}
	return value
}

func canonicalAudioCodec(value string) string {
	value = strings.ToUpper(canonicalToken(value))
	value = trimAudioChannelSuffix(value)
	value = strings.ReplaceAll(value, "-", "")
	switch value {
	case "TRUEHD":
		return "TrueHD"
	case "ATMOS":
		return "Atmos"
	case "EAC3", "DDP", "DD+":
		return "DDP"
	case "AC3":
		return "AC3"
	case "DTSHDMA", "DTSHD":
		return "DTS-HD MA"
	case "DTSHDHR":
		return "DTS-HD"
	case "DTSX":
		return "DTS:X"
	case "DTS":
		return "DTS"
	case "AAC":
		return "AAC"
	case "FLAC":
		return "FLAC"
	case "LPCM", "PCM":
		return "LPCM"
	case "OPUS":
		return "Opus"
	case "VORBIS":
		return "Vorbis"
	case "MP3":
		return "MP3"
	default:
		return ""
	}
}

func trimAudioChannelSuffix(value string) string {
	return strings.TrimRight(audioSuffixRE.ReplaceAllString(value, ""), " ._-")
}

func canonicalAudioChannels(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "2ch":
		return "2.0"
	case "6ch":
		return "5.1"
	case "8ch":
		return "7.1"
	default:
		return value
	}
}

func canonicalHDR(value string) string {
	normalized := strings.ToLower(canonicalToken(value))
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "dolbyvision", "dovi", "dv":
		return "DV"
	case "hdr10+":
		return "HDR10+"
	case "hdr10", "hdr":
		return "HDR10"
	case "hlg":
		return "HLG"
	case "sdr":
		return "SDR"
	default:
		return ""
	}
}

func canonicalEdition(value string) string {
	value = canonicalToken(value)
	normalized := strings.ToLower(strings.ReplaceAll(value, "-", ""))
	switch normalized {
	case "directorscut":
		return "Directors Cut"
	case "openmatte":
		return "Open Matte"
	case "ultimatecut":
		return "Ultimate Cut"
	case "finalcut":
		return "Final Cut"
	default:
		return strings.TrimSpace(strings.ReplaceAll(value, "-", " "))
	}
}

func canonicalReleaseGroup(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " ._-[](){}")
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func validReleaseGroup(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || len(value) > 40 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '&' || r == '.' {
			continue
		}
		if (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			continue
		}
		return false
	}
	return true
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Unknown"
	}
	return value
}

func two(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueNonEmpty(values ...string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsASCII(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}

func containsHan(value string) bool {
	for _, r := range value {
		if (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			return true
		}
	}
	return false
}

func reverse(values []string) []string {
	result := make([]string, 0, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		result = append(result, values[i])
	}
	return result
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
