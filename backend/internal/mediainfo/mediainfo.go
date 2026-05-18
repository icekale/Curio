package mediainfo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Unknown      = "Unknown"
	isoHeaderMax = int64(16 * 1024 * 1024)
	isoSampleMax = int64(64 * 1024 * 1024)
)

var (
	ffprobeSlots  = make(chan struct{}, 2)
	isoSampleStep = []int64{8 * 1024 * 1024, 16 * 1024 * 1024, 32 * 1024 * 1024, isoSampleMax}
)

type Info struct {
	Resolution    string
	VideoCodec    string
	AudioCodec    string
	AudioChannels string
	HDRFormat     string
}

type DetailedInfo struct {
	Info          Info
	Streams       []Stream
	DurationTicks int64
}

type Stream struct {
	Index            int
	Type             string
	Codec            string
	CodecTag         string
	Profile          string
	Width            int
	Height           int
	CodedWidth       int
	CodedHeight      int
	Channels         int
	ChannelLayout    string
	SampleRate       int
	BitRate          int64
	AverageFrameRate string
	RealFrameRate    string
	Language         string
	Title            string
	Default          bool
	Forced           bool
	HearingImpaired  bool
	TextSubtitle     bool
	VideoRange       string
}

type ProbeStats struct {
	Total          time.Duration
	ISOParse       time.Duration
	ISOSample      time.Duration
	FFProbe        time.Duration
	RangeRead      time.Duration
	RangeRequests  int
	RangeBytes     int64
	ISOAttempts    int
	ISOSampleBytes int64
}

type Source struct {
	Path      string
	URL       string
	Headers   map[string]string
	UserAgent string
	Extension string
	Size      int64
}

type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	CodecTag       string            `json:"codec_tag_string"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	CodedWidth     int               `json:"coded_width"`
	CodedHeight    int               `json:"coded_height"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	SampleRate     string            `json:"sample_rate"`
	BitRate        string            `json:"bit_rate"`
	Duration       string            `json:"duration"`
	AvgFrameRate   string            `json:"avg_frame_rate"`
	RFrameRate     string            `json:"r_frame_rate"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	ColorSpace     string            `json:"color_space"`
	PixFmt         string            `json:"pix_fmt"`
	Disposition    map[string]int    `json:"disposition"`
	Tags           map[string]string `json:"tags"`
	SideDataList   []map[string]any  `json:"side_data_list"`
}

type probeFormat struct {
	Duration string `json:"duration"`
}

func UnknownInfo() Info {
	return Info{
		Resolution:    Unknown,
		VideoCodec:    Unknown,
		AudioCodec:    Unknown,
		AudioChannels: Unknown,
		HDRFormat:     Unknown,
	}
}

func Probe(ctx context.Context, source Source) (Info, error) {
	info, _, err := ProbeWithStats(ctx, source)
	return info, err
}

func ProbeWithStats(ctx context.Context, source Source) (Info, ProbeStats, error) {
	detailed, stats, err := ProbeDetailedWithStats(ctx, source)
	return detailed.Info, stats, err
}

func ProbeDetailed(ctx context.Context, source Source) (DetailedInfo, error) {
	info, _, err := ProbeDetailedWithStats(ctx, source)
	return info, err
}

func ProbeDetailedWithStats(ctx context.Context, source Source) (DetailedInfo, ProbeStats, error) {
	var stats ProbeStats
	start := time.Now()
	info, err := probeDetailed(ctx, source, &stats)
	stats.Total = time.Since(start)
	return info, stats, err
}

func probe(ctx context.Context, source Source, stats *ProbeStats) (Info, error) {
	detailed, err := probeDetailed(ctx, source, stats)
	return detailed.Info, err
}

func probeDetailed(ctx context.Context, source Source, stats *ProbeStats) (DetailedInfo, error) {
	if strings.EqualFold(strings.TrimPrefix(source.Extension, "."), "iso") {
		return probeISODetailed(ctx, source, stats)
	}
	input, cleanup, err := probeInput(ctx, source)
	if err != nil {
		return DetailedInfo{Info: UnknownInfo()}, err
	}
	defer cleanup()
	return probeFFDetailed(ctx, input, source.Headers, source.UserAgent, stats)
}

func probeISO(ctx context.Context, source Source, stats *ProbeStats) (Info, error) {
	detailed, err := probeISODetailed(ctx, source, stats)
	return detailed.Info, err
}

func probeISODetailed(ctx context.Context, source Source, stats *ProbeStats) (DetailedInfo, error) {
	reader, cleanup, err := isoReader(ctx, source, stats)
	if err != nil {
		return DetailedInfo{Info: UnknownInfo()}, err
	}
	defer cleanup()
	if strings.TrimSpace(source.URL) != "" {
		reader = newHeaderCacheReader(reader, isoHeaderMax)
	}
	parser := &udfParser{
		reader:          reader,
		size:            source.Size,
		blockSize:       2048,
		partitions:      map[uint16]uint32{},
		partitionRefs:   map[uint16]uint16{},
		metadataRefs:    map[uint16]metadataPartition{},
		metadataEntries: map[uint16]isoEntry{},
		headerReadLimit: isoHeaderMax,
	}
	parseStart := time.Now()
	stream, err := parser.mainStream(ctx)
	if stats != nil {
		stats.ISOParse += time.Since(parseStart)
	}
	if err != nil {
		return DetailedInfo{Info: UnknownInfo()}, err
	}
	if len(stream.extents) == 0 {
		return DetailedInfo{Info: UnknownInfo()}, fmt.Errorf("ISO 内未找到可采样的主媒体流：%s", stream.path)
	}
	temp, err := os.CreateTemp("", "curio-iso-*.m2ts")
	if err != nil {
		return DetailedInfo{Info: UnknownInfo()}, err
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()
	var lastInfo DetailedInfo
	var lastErr error
	var currentSize int64
	for _, sampleLimit := range isoSampleStep {
		sampleStart := time.Now()
		err := extendISOSample(ctx, reader, parser, stream, temp, &currentSize, sampleLimit)
		if stats != nil {
			stats.ISOSample += time.Since(sampleStart)
			stats.ISOSampleBytes = currentSize
		}
		if err != nil {
			return DetailedInfo{Info: UnknownInfo()}, err
		}
		if info, err := temp.Stat(); err != nil || info.Size() < min64(sampleLimit, 2*1024*1024) {
			if err != nil {
				return DetailedInfo{Info: UnknownInfo()}, err
			}
			return DetailedInfo{Info: UnknownInfo()}, errors.New("ISO 主媒体流采样不足 2MB")
		}
		if err := temp.Sync(); err != nil {
			return DetailedInfo{Info: UnknownInfo()}, err
		}
		if stats != nil {
			stats.ISOAttempts++
		}
		info, probeErr := probeFFDetailed(ctx, temp.Name(), nil, "", stats)
		if probeErr == nil {
			lastInfo = info
		}
		lastErr = probeErr
	}
	if lastErr != nil {
		return DetailedInfo{Info: UnknownInfo()}, lastErr
	}
	lastInfo.DurationTicks = 0
	return lastInfo, nil
}

func probeInput(ctx context.Context, source Source) (string, func(), error) {
	if strings.TrimSpace(source.Path) != "" {
		return source.Path, func() {}, nil
	}
	if strings.TrimSpace(source.URL) != "" {
		return source.URL, func() {}, nil
	}
	return "", func() {}, errors.New("媒体源为空")
}

func probeFF(ctx context.Context, input string, headers map[string]string, userAgent string, stats *ProbeStats) (Info, error) {
	detailed, err := probeFFDetailed(ctx, input, headers, userAgent, stats)
	return detailed.Info, err
}

func probeFFDetailed(ctx context.Context, input string, headers map[string]string, userAgent string, stats *ProbeStats) (DetailedInfo, error) {
	select {
	case ffprobeSlots <- struct{}{}:
		defer func() { <-ffprobeSlots }()
	case <-ctx.Done():
		return DetailedInfo{Info: UnknownInfo()}, ctx.Err()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{
		"-v", "error",
		"-probesize", "64M",
		"-analyzeduration", "10M",
	}
	if strings.HasPrefix(strings.ToLower(input), "http://") || strings.HasPrefix(strings.ToLower(input), "https://") {
		args = append(args, "-rw_timeout", "30000000")
		if strings.TrimSpace(userAgent) != "" {
			args = append(args, "-user_agent", userAgent)
		}
		if len(headers) > 0 {
			args = append(args, "-headers", httpHeaderBlock(headers))
		}
	}
	args = append(args, "-show_streams", "-show_format", "-of", "json", input)
	cmd := exec.CommandContext(probeCtx, "ffprobe", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	probeStart := time.Now()
	if err := cmd.Run(); err != nil {
		if stats != nil {
			stats.FFProbe += time.Since(probeStart)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return DetailedInfo{Info: UnknownInfo()}, fmt.Errorf("ffprobe 读取失败：%s", message)
	}
	if stats != nil {
		stats.FFProbe += time.Since(probeStart)
	}
	var output probeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return DetailedInfo{Info: UnknownInfo()}, err
	}
	return normalizeDetailed(output), nil
}

func normalizeDetailed(output probeOutput) DetailedInfo {
	return DetailedInfo{
		Info:          normalize(output),
		Streams:       streamDetails(output.Streams),
		DurationTicks: durationTicks(output),
	}
}

func durationTicks(output probeOutput) int64 {
	seconds := durationSeconds(output.Format.Duration)
	for _, stream := range output.Streams {
		if value := durationSeconds(stream.Duration); value > seconds {
			seconds = value
		}
	}
	if seconds <= 0 {
		return 0
	}
	return int64(math.Round(seconds * 10000000))
}

func durationSeconds(value string) float64 {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	return seconds
}

func normalize(output probeOutput) Info {
	info := UnknownInfo()
	video, hasVideo := primaryVideo(output.Streams)
	if hasVideo {
		info.Resolution = resolution(video)
		info.VideoCodec = videoCodec(video)
		info.HDRFormat = hdrFormat(output.Streams)
	}
	audio, hasAudio := primaryAudio(output.Streams)
	if hasAudio {
		info.AudioCodec = audioCodec(audio)
		info.AudioChannels = audioChannels(audio)
	}
	return info
}

func streamDetails(streams []probeStream) []Stream {
	out := make([]Stream, 0, len(streams))
	for _, stream := range streams {
		switch stream.CodecType {
		case "video", "audio", "subtitle":
		default:
			continue
		}
		detail := Stream{
			Index:            stream.Index,
			Type:             stream.CodecType,
			Codec:            strings.ToLower(strings.TrimSpace(stream.CodecName)),
			CodecTag:         strings.TrimSpace(stream.CodecTag),
			Profile:          strings.TrimSpace(stream.Profile),
			Width:            stream.Width,
			Height:           stream.Height,
			CodedWidth:       stream.CodedWidth,
			CodedHeight:      stream.CodedHeight,
			Channels:         stream.Channels,
			ChannelLayout:    strings.TrimSpace(stream.ChannelLayout),
			SampleRate:       intString(stream.SampleRate),
			BitRate:          int64String(stream.BitRate),
			AverageFrameRate: cleanFrameRate(stream.AvgFrameRate),
			RealFrameRate:    cleanFrameRate(stream.RFrameRate),
			Language:         streamLanguage(stream.Tags),
			Title:            streamTitle(stream.Tags),
			Default:          stream.Disposition["default"] == 1,
			Forced:           stream.Disposition["forced"] == 1,
			HearingImpaired:  stream.Disposition["hearing_impaired"] == 1,
		}
		if detail.Type == "video" {
			detail.VideoRange = videoRange(stream)
		}
		if detail.Type == "subtitle" {
			detail.TextSubtitle = textSubtitleCodec(detail.Codec)
		}
		out = append(out, detail)
	}
	return out
}

func primaryVideo(streams []probeStream) (probeStream, bool) {
	for _, stream := range streams {
		if stream.CodecType == "video" {
			return stream, true
		}
	}
	return probeStream{}, false
}

func primaryAudio(streams []probeStream) (probeStream, bool) {
	var candidates []probeStream
	for _, stream := range streams {
		if stream.CodecType == "audio" {
			candidates = append(candidates, stream)
		}
	}
	if len(candidates) == 0 {
		return probeStream{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		di := candidates[i].Disposition["default"]
		dj := candidates[j].Disposition["default"]
		if di != dj {
			return di > dj
		}
		return candidates[i].Channels > candidates[j].Channels
	})
	return candidates[0], true
}

func resolution(stream probeStream) string {
	height := stream.Height
	if height == 0 {
		height = stream.CodedHeight
	}
	width := stream.Width
	if width == 0 {
		width = stream.CodedWidth
	}
	switch {
	case height >= 4300 || width >= 7600:
		return "4320p"
	case height >= 2000 || width >= 3800:
		return "2160p"
	case height >= 1000 || width >= 1900:
		return "1080p"
	case height >= 700 || width >= 1200:
		return "720p"
	case height > 0:
		return strconv.Itoa(height) + "p"
	default:
		return Unknown
	}
}

func videoCodec(stream probeStream) string {
	value := strings.ToLower(strings.TrimSpace(stream.CodecName))
	switch value {
	case "h264", "avc1", "avc":
		return "AVC"
	case "hevc", "h265", "hev1", "hvc1":
		return "HEVC"
	case "h266", "vvc":
		return "VVC"
	case "av1":
		return "AV1"
	case "mpeg2video":
		return "MPEG-2"
	case "mpeg4":
		return "MPEG-4"
	case "vc1":
		return "VC-1"
	case "vp9":
		return "VP9"
	default:
		if value == "" {
			return Unknown
		}
		return strings.ToUpper(value)
	}
}

func audioCodec(stream probeStream) string {
	value := strings.ToLower(strings.TrimSpace(stream.CodecName))
	profile := strings.ToLower(strings.TrimSpace(stream.Profile + " " + tagText(stream.Tags)))
	switch value {
	case "truehd":
		return "TrueHD"
	case "eac3":
		return "DDP"
	case "ac3":
		return "AC3"
	case "dts":
		switch {
		case strings.Contains(profile, "dts-hd ma"), strings.Contains(profile, "dts hd ma"), strings.Contains(profile, "ma"):
			return "DTS-HD MA"
		case strings.Contains(profile, "dts-hd hra"), strings.Contains(profile, "dts hd hra"), strings.Contains(profile, "hra"):
			return "DTS-HD HRA"
		case strings.Contains(profile, "dts:x"), strings.Contains(profile, "dts-x"):
			return "DTS-X"
		default:
			return "DTS"
		}
	case "aac":
		return "AAC"
	case "flac":
		return "FLAC"
	case "alac":
		return "ALAC"
	case "opus":
		return "Opus"
	case "mp3":
		return "MP3"
	case "pcm_bluray", "pcm_s16le", "pcm_s24le", "pcm_s32le":
		return "LPCM"
	default:
		if value == "" {
			return Unknown
		}
		return strings.ToUpper(value)
	}
}

func audioChannels(stream probeStream) string {
	layout := strings.ToLower(strings.TrimSpace(stream.ChannelLayout))
	for _, token := range []string{"7.1.4", "7.1", "6.1", "5.1.4", "5.1.2", "5.1", "4.0", "3.1", "3.0", "2.1", "2.0", "1.0"} {
		if strings.Contains(layout, token) {
			return token
		}
	}
	switch stream.Channels {
	case 8:
		return "7.1"
	case 7:
		return "6.1"
	case 6:
		return "5.1"
	case 5:
		return "5.0"
	case 4:
		return "4.0"
	case 3:
		return "3.0"
	case 2:
		return "2.0"
	case 1:
		return "1.0"
	default:
		if stream.Channels > 0 {
			return strconv.Itoa(stream.Channels)
		}
		return Unknown
	}
}

func hdrFormat(streams []probeStream) string {
	values := make([]string, 0)
	for _, stream := range streams {
		if stream.CodecType != "video" {
			continue
		}
		values = append(values,
			stream.Profile,
			stream.ColorTransfer,
			stream.ColorPrimaries,
			stream.ColorSpace,
			stream.PixFmt,
			tagText(stream.Tags),
			sideDataText(stream.SideDataList),
		)
	}
	text := strings.ToLower(strings.Join(values, " "))
	formats := make([]string, 0, 2)
	if hasAny(text, "dovi", "dolby vision", "dv profile", "dvhe", "dvh1") {
		formats = append(formats, "DV")
	}
	if hasAny(text, "hdr10+", "smpte2094", "dynamic hdr plus") {
		formats = append(formats, "HDR10+")
	}
	if hasAny(text, "smpte2084", "arib-std-b67") {
		if strings.Contains(text, "arib-std-b67") {
			formats = append(formats, "HLG")
		} else if !contains(formats, "HDR10+") {
			formats = append(formats, "HDR10")
		}
	}
	if len(formats) == 0 {
		return "SDR"
	}
	return strings.Join(unique(formats), " ")
}

func videoRange(stream probeStream) string {
	text := strings.ToLower(strings.Join([]string{
		stream.Profile,
		stream.ColorTransfer,
		stream.ColorPrimaries,
		stream.ColorSpace,
		stream.PixFmt,
		tagText(stream.Tags),
		sideDataText(stream.SideDataList),
	}, " "))
	switch {
	case hasAny(text, "dovi", "dolby vision", "dv profile", "dvhe", "dvh1"):
		return "DV"
	case hasAny(text, "hdr10+", "smpte2094", "dynamic hdr plus"):
		return "HDR10Plus"
	case strings.Contains(text, "smpte2084"):
		return "HDR10"
	case strings.Contains(text, "arib-std-b67"):
		return "HLG"
	default:
		return "SDR"
	}
}

func streamLanguage(tags map[string]string) string {
	for _, key := range []string{"language", "LANGUAGE"} {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value
		}
	}
	return "und"
}

func streamTitle(tags map[string]string) string {
	for _, key := range []string{"title", "TITLE", "handler_name"} {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value
		}
	}
	return ""
}

func textSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text":
		return true
	default:
		return false
	}
}

func intString(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func int64String(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func cleanFrameRate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0/0" {
		return ""
	}
	return value
}

func tagText(tags map[string]string) string {
	values := make([]string, 0, len(tags))
	for _, value := range tags {
		values = append(values, value)
	}
	return strings.Join(values, " ")
}

func sideDataText(items []map[string]any) string {
	values := make([]string, 0, len(items)*2)
	for _, item := range items {
		for key, value := range item {
			values = append(values, key, fmt.Sprint(value))
		}
	}
	return strings.Join(values, " ")
}

func httpHeaderBlock(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(headers[key])
		b.WriteString("\r\n")
	}
	return b.String()
}

func hasAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func unique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

type httpRangeReader struct {
	url       string
	headers   map[string]string
	userAgent string
	client    *http.Client
	stats     *ProbeStats
}

func isoReader(ctx context.Context, source Source, stats *ProbeStats) (io.ReaderAt, func(), error) {
	if strings.TrimSpace(source.Path) != "" {
		file, err := os.Open(source.Path)
		if err != nil {
			return nil, func() {}, err
		}
		return file, func() { _ = file.Close() }, nil
	}
	if strings.TrimSpace(source.URL) == "" {
		return nil, func() {}, errors.New("ISO 媒体源为空")
	}
	_ = ctx
	return &httpRangeReader{
		url:       source.URL,
		headers:   source.Headers,
		userAgent: source.UserAgent,
		client:    &http.Client{Timeout: 45 * time.Second},
		stats:     stats,
	}, func() {}, nil
}

func (r *httpRangeReader) ReadAt(p []byte, offset int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	start := time.Now()
	var n int
	defer func() {
		if r.stats != nil {
			r.stats.RangeRequests++
			r.stats.RangeBytes += int64(n)
			r.stats.RangeRead += time.Since(start)
		}
	}()
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return 0, err
	}
	for key, value := range r.headers {
		req.Header.Set(key, value)
	}
	if strings.TrimSpace(r.userAgent) != "" {
		req.Header.Set("User-Agent", r.userAgent)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+int64(len(p))-1))
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && !(offset == 0 && resp.StatusCode == http.StatusOK) {
		return 0, fmt.Errorf("HTTP Range 读取失败：%s", resp.Status)
	}
	n, err = io.ReadFull(resp.Body, p)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return n, io.EOF
	}
	return n, err
}

type headerCacheReader struct {
	base   io.ReaderAt
	max    int64
	loaded int64
	cache  []byte
}

func newHeaderCacheReader(base io.ReaderAt, max int64) io.ReaderAt {
	if max <= 0 {
		return base
	}
	return &headerCacheReader{base: base, max: max}
}

func (r *headerCacheReader) ReadAt(p []byte, offset int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	end := offset + int64(len(p))
	if offset < 0 || end > r.max {
		return r.base.ReadAt(p, offset)
	}
	if err := r.ensure(end); err != nil && r.loaded < end {
		return 0, err
	}
	n := copy(p, r.cache[offset:end])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *headerCacheReader) ensure(need int64) error {
	if need <= r.loaded {
		return nil
	}
	target := int64(2 * 1024 * 1024)
	for target < need && target < r.max {
		target *= 2
	}
	if target > r.max {
		target = r.max
	}
	next := make([]byte, int(target))
	copy(next, r.cache)
	n, err := r.base.ReadAt(next[r.loaded:target], r.loaded)
	r.loaded += int64(n)
	r.cache = next[:r.loaded]
	if err != nil && !(errors.Is(err, io.EOF) && n > 0) {
		return err
	}
	return nil
}

type isoExtent struct {
	location uint32
	length   uint32
	part     uint16
}

type isoEntry struct {
	name      string
	path      string
	fileType  byte
	size      int64
	extents   []isoExtent
	inline    []byte
	directory bool
}

type metadataPartition struct {
	partitionNumber uint16
	fileLocation    uint32
}

type udfParser struct {
	reader          io.ReaderAt
	size            int64
	blockSize       int64
	partitions      map[uint16]uint32
	partitionRefs   map[uint16]uint16
	metadataRefs    map[uint16]metadataPartition
	metadataEntries map[uint16]isoEntry
	fileSetExtent   isoExtent
	headerReadLimit int64
}

func sampleISO(ctx context.Context, reader io.ReaderAt, size, sampleLimit int64) (string, error) {
	parser := &udfParser{
		reader:          reader,
		size:            size,
		blockSize:       2048,
		partitions:      map[uint16]uint32{},
		partitionRefs:   map[uint16]uint16{},
		metadataRefs:    map[uint16]metadataPartition{},
		metadataEntries: map[uint16]isoEntry{},
		headerReadLimit: isoHeaderMax,
	}
	stream, err := parser.mainStream(ctx)
	if err != nil {
		return "", err
	}
	if len(stream.extents) == 0 {
		return "", fmt.Errorf("ISO 内未找到可采样的主媒体流：%s", stream.path)
	}
	sampleLength := min64(sampleLimit, stream.size)
	if sampleLength <= 0 {
		sampleLength = sampleLimit
	}
	temp, err := os.CreateTemp("", "curio-iso-*.m2ts")
	if err != nil {
		return "", err
	}
	defer temp.Close()
	remaining := sampleLength
	buf := make([]byte, 1024*1024)
	for _, extent := range stream.extents {
		if remaining <= 0 {
			break
		}
		offset, err := parser.physicalOffset(extent)
		if err != nil {
			_ = os.Remove(temp.Name())
			return "", err
		}
		toRead := min64(int64(extent.length), remaining)
		for read := int64(0); read < toRead; {
			if err := ctx.Err(); err != nil {
				_ = os.Remove(temp.Name())
				return "", err
			}
			chunk := min64(int64(len(buf)), toRead-read)
			n, err := reader.ReadAt(buf[:int(chunk)], offset+read)
			if n > 0 {
				if _, writeErr := temp.Write(buf[:n]); writeErr != nil {
					_ = os.Remove(temp.Name())
					return "", writeErr
				}
				read += int64(n)
				remaining -= int64(n)
			}
			if err != nil {
				if errors.Is(err, io.EOF) && n > 0 {
					break
				}
				_ = os.Remove(temp.Name())
				return "", err
			}
		}
	}
	if info, err := temp.Stat(); err != nil || info.Size() < min64(sampleLimit, 2*1024*1024) {
		_ = os.Remove(temp.Name())
		if err != nil {
			return "", err
		}
		return "", errors.New("ISO 主媒体流采样不足 2MB")
	}
	return temp.Name(), nil
}

func extendISOSample(ctx context.Context, reader io.ReaderAt, parser *udfParser, stream isoEntry, temp *os.File, currentSize *int64, sampleLimit int64) error {
	target := min64(sampleLimit, stream.size)
	if target <= 0 {
		target = sampleLimit
	}
	if *currentSize >= target {
		return nil
	}
	if _, err := temp.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	skip := *currentSize
	remaining := target - *currentSize
	buf := make([]byte, 4*1024*1024)
	for _, extent := range stream.extents {
		if remaining <= 0 {
			break
		}
		extentLength := int64(extent.length)
		if skip >= extentLength {
			skip -= extentLength
			continue
		}
		offset, err := parser.physicalOffset(extent)
		if err != nil {
			return err
		}
		readStart := skip
		skip = 0
		toRead := min64(extentLength-readStart, remaining)
		for read := int64(0); read < toRead; {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunk := min64(int64(len(buf)), toRead-read)
			n, err := reader.ReadAt(buf[:int(chunk)], offset+readStart+read)
			if n > 0 {
				if _, writeErr := temp.Write(buf[:n]); writeErr != nil {
					return writeErr
				}
				read += int64(n)
				remaining -= int64(n)
				*currentSize += int64(n)
			}
			if err != nil {
				if errors.Is(err, io.EOF) && n > 0 {
					break
				}
				return err
			}
		}
	}
	return nil
}

func (p *udfParser) mainStream(ctx context.Context) (isoEntry, error) {
	if err := p.loadVolume(); err != nil {
		return isoEntry{}, err
	}
	root, err := p.rootEntry()
	if err != nil {
		return isoEntry{}, err
	}
	var streams []isoEntry
	var scanned []string
	if err := p.walk(ctx, root, 0, func(entry isoEntry) {
		if len(scanned) < 12 {
			scanned = append(scanned, entry.path)
		}
		if strings.EqualFold(filepath.Ext(entry.name), ".m2ts") || strings.EqualFold(filepath.Ext(entry.name), ".mts") {
			streams = append(streams, entry)
		}
	}); err != nil {
		return isoEntry{}, err
	}
	if len(streams) == 0 {
		return isoEntry{}, fmt.Errorf("ISO 内未找到 m2ts 主媒体流，root type=%d size=%d extents=%d inline=%d，已扫描 %d 个文件，样例：%s", root.fileType, root.size, len(root.extents), len(root.inline), len(scanned), strings.Join(scanned, ", "))
	}
	sort.SliceStable(streams, func(i, j int) bool {
		if strings.Contains(strings.ToLower(streams[i].path), "/bdmv/stream/") != strings.Contains(strings.ToLower(streams[j].path), "/bdmv/stream/") {
			return strings.Contains(strings.ToLower(streams[i].path), "/bdmv/stream/")
		}
		return streams[i].size > streams[j].size
	})
	return streams[0], nil
}

func (p *udfParser) loadVolume() error {
	anchor, err := p.readBlock(256)
	if err != nil {
		return err
	}
	if tagID(anchor) != 2 {
		return errors.New("ISO 不是可识别的 UDF 镜像")
	}
	main := extentAD(anchor[16:24])
	if main.length == 0 {
		return errors.New("UDF 主卷描述符为空")
	}
	blocks := int(min64(int64(main.length)/p.blockSize, 512))
	for i := 0; i < blocks; i++ {
		block, err := p.readBlock(int64(main.location) + int64(i))
		if err != nil {
			return err
		}
		switch tagID(block) {
		case 5:
			partNumber := le16(block[22:24])
			p.partitions[partNumber] = le32(block[188:192])
		case 6:
			p.blockSize = int64(le32(block[212:216]))
			if p.blockSize <= 0 {
				p.blockSize = 2048
			}
			p.fileSetExtent = longAD(block[248:264])
			p.loadPartitionMaps(block)
		case 8:
			i = blocks
		}
	}
	if len(p.partitions) == 0 || p.fileSetExtent.length == 0 {
		return errors.New("UDF 卷描述符缺少分区或文件集信息")
	}
	return nil
}

func (p *udfParser) rootEntry() (isoEntry, error) {
	offset, err := p.physicalOffset(p.fileSetExtent)
	if err != nil {
		return isoEntry{}, err
	}
	fsd, err := p.readAt(offset, p.blockSize, true)
	if err != nil {
		return isoEntry{}, err
	}
	if tagID(fsd) != 256 {
		fsd, err = p.findDescriptor(256)
		if err != nil {
			return isoEntry{}, errors.New("UDF 文件集描述符无效")
		}
	}
	rootICB := longAD(fsd[400:416])
	root, err := p.entry(rootICB, "")
	if err != nil {
		offset, _ := p.physicalOffset(rootICB)
		return isoEntry{}, fmt.Errorf("UDF 根目录 ICB 无效 part=%d location=%d offset=%d: %w", rootICB.part, rootICB.location, offset, err)
	}
	root.directory = true
	return root, nil
}

func (p *udfParser) findDescriptor(target uint16) ([]byte, error) {
	blocks := p.headerReadLimit / p.blockSize
	for block := int64(0); block < blocks; block++ {
		data, err := p.readBlock(block)
		if err != nil {
			continue
		}
		if tagID(data) == target {
			return data, nil
		}
	}
	return nil, fmt.Errorf("UDF 描述符不存在：%d", target)
}

func (p *udfParser) loadPartitionMaps(block []byte) {
	if len(block) < 440 {
		return
	}
	mapLength := int(le32(block[264:268]))
	mapCount := int(le32(block[268:272]))
	offset := 440
	for index := 0; index < mapCount && offset+2 <= len(block) && offset < 440+mapLength; index++ {
		mapType := block[offset]
		itemLength := int(block[offset+1])
		if itemLength <= 0 || offset+itemLength > len(block) {
			return
		}
		item := block[offset : offset+itemLength]
		switch {
		case mapType == 1 && len(item) >= 6:
			p.partitionRefs[uint16(index)] = le16(item[4:6])
		case mapType == 2 && len(item) >= 40:
			partNumber := le16(item[38:40])
			p.partitionRefs[uint16(index)] = partNumber
			if len(item) >= 44 && strings.Contains(string(item[4:36]), "Metadata Partition") {
				p.metadataRefs[uint16(index)] = metadataPartition{
					partitionNumber: partNumber,
					fileLocation:    le32(item[40:44]),
				}
			}
		}
		offset += itemLength
	}
}

func (p *udfParser) walk(ctx context.Context, entry isoEntry, depth int, visit func(isoEntry)) error {
	if depth > 16 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !entry.directory {
		visit(entry)
		return nil
	}
	children, err := p.children(entry)
	if err != nil {
		return err
	}
	for _, child := range children {
		visit(child)
		if child.directory {
			if err := p.walk(ctx, child, depth+1, visit); err != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func (p *udfParser) children(entry isoEntry) ([]isoEntry, error) {
	data, err := p.entryData(entry, 4*1024*1024)
	if err != nil {
		return nil, err
	}
	children := make([]isoEntry, 0)
	for offset := 0; offset+38 <= len(data); {
		record := data[offset:]
		if tagID(record) != 257 {
			offset += 4
			continue
		}
		nameLen := int(record[19])
		implLen := int(le16(record[36:38]))
		total := align4(38 + implLen + nameLen)
		if total <= 0 || total > len(record) {
			break
		}
		flags := record[18]
		name := decodeUDFName(record[38+implLen : 38+implLen+nameLen])
		offset += total
		if name == "" || flags&0x08 != 0 {
			continue
		}
		child, err := p.entry(longAD(record[20:36]), joinISOPath(entry.path, name))
		if err != nil {
			continue
		}
		child.name = name
		child.directory = child.fileType == 4 || flags&0x02 != 0
		children = append(children, child)
	}
	return children, nil
}

func (p *udfParser) entry(icb isoExtent, fullPath string) (isoEntry, error) {
	offset, err := p.physicalOffset(icb)
	if err != nil {
		return isoEntry{}, err
	}
	data, err := p.readAt(offset, p.blockSize, true)
	if err != nil {
		return isoEntry{}, err
	}
	switch tagID(data) {
	case 261:
		return p.fileEntry(data, fullPath, false, icb.part), nil
	case 266:
		return p.fileEntry(data, fullPath, true, icb.part), nil
	default:
		return isoEntry{}, fmt.Errorf("UDF 文件项无效：tag=%d", tagID(data))
	}
}

func (p *udfParser) fileEntry(data []byte, fullPath string, extended bool, defaultPart uint16) isoEntry {
	name := filepath.Base(fullPath)
	if fullPath == "" {
		name = ""
	}
	fileType := data[27]
	flags := le16(data[34:36])
	allocType := flags & 0x0007
	infoOffset := 56
	eaOffset := 168
	adLenOffset := 172
	adStart := 176
	if extended {
		infoOffset = 56
		eaOffset = 208
		adLenOffset = 212
		adStart = 216
	}
	size := int64(le64(data[infoOffset : infoOffset+8]))
	eaLen := int(le32(data[eaOffset : eaOffset+4]))
	adLen := int(le32(data[adLenOffset : adLenOffset+4]))
	start := adStart + eaLen
	end := start + adLen
	entry := isoEntry{name: name, path: fullPath, fileType: fileType, size: size, directory: fileType == 4}
	if start > len(data) || end > len(data) {
		return entry
	}
	descriptors := data[start:end]
	switch allocType {
	case 0:
		entry.extents = parseShortAD(descriptors, defaultPart)
	case 1:
		entry.extents = parseLongAD(descriptors)
	case 3:
		entry.inline = append([]byte(nil), descriptors...)
	}
	return entry
}

func (p *udfParser) entryData(entry isoEntry, limit int64) ([]byte, error) {
	if len(entry.inline) > 0 {
		return entry.inline, nil
	}
	var out bytes.Buffer
	remaining := min64(entry.size, limit)
	if remaining <= 0 {
		remaining = limit
	}
	for _, extent := range entry.extents {
		if remaining <= 0 {
			break
		}
		offset, err := p.physicalOffset(extent)
		if err != nil {
			return nil, err
		}
		chunk := min64(int64(extent.length), remaining)
		data, err := p.readAt(offset, chunk, true)
		if err != nil {
			return nil, err
		}
		out.Write(data)
		remaining -= int64(len(data))
	}
	return out.Bytes(), nil
}

func (p *udfParser) physicalOffset(ext isoExtent) (int64, error) {
	if meta, ok := p.metadataRefs[ext.part]; ok {
		return p.metadataOffset(ext.part, meta, int64(ext.location)*p.blockSize)
	}
	part := ext.part
	if mapped, ok := p.partitionRefs[ext.part]; ok {
		part = mapped
	}
	start, ok := p.partitions[part]
	if !ok && len(p.partitions) == 1 {
		for _, onlyStart := range p.partitions {
			start = onlyStart
			ok = true
		}
	}
	if !ok {
		return 0, fmt.Errorf("UDF 分区不存在：%d", ext.part)
	}
	return int64(start+ext.location) * p.blockSize, nil
}

func (p *udfParser) metadataOffset(ref uint16, meta metadataPartition, logicalOffset int64) (int64, error) {
	entry, err := p.metadataEntry(ref, meta)
	if err != nil {
		return 0, err
	}
	remaining := logicalOffset
	for _, extent := range entry.extents {
		length := int64(extent.length)
		if remaining >= length {
			remaining -= length
			continue
		}
		offset, err := p.physicalOffset(extent)
		if err != nil {
			return 0, err
		}
		return offset + remaining, nil
	}
	return 0, fmt.Errorf("UDF 元数据分区越界：ref=%d offset=%d", ref, logicalOffset)
}

func (p *udfParser) metadataEntry(ref uint16, meta metadataPartition) (isoEntry, error) {
	if entry, ok := p.metadataEntries[ref]; ok {
		return entry, nil
	}
	start, ok := p.partitions[meta.partitionNumber]
	if !ok && len(p.partitions) == 1 {
		for _, onlyStart := range p.partitions {
			start = onlyStart
			ok = true
		}
	}
	if !ok {
		return isoEntry{}, fmt.Errorf("UDF 元数据物理分区不存在：%d", meta.partitionNumber)
	}
	offset := int64(start+meta.fileLocation) * p.blockSize
	data, err := p.readAt(offset, p.blockSize, true)
	if err != nil {
		return isoEntry{}, err
	}
	var entry isoEntry
	switch tagID(data) {
	case 261:
		entry = p.fileEntry(data, "$metadata", false, meta.partitionNumber)
	case 266:
		entry = p.fileEntry(data, "$metadata", true, meta.partitionNumber)
	default:
		return isoEntry{}, fmt.Errorf("UDF 元数据文件项无效：tag=%d", tagID(data))
	}
	p.metadataEntries[ref] = entry
	return entry, nil
}

func (p *udfParser) readBlock(block int64) ([]byte, error) {
	return p.readAt(block*p.blockSize, p.blockSize, true)
}

func (p *udfParser) readAt(offset, length int64, header bool) ([]byte, error) {
	if header && offset+length > p.headerReadLimit {
		return nil, fmt.Errorf("UDF 头部读取超过限制：%d", offset+length)
	}
	if length <= 0 {
		return nil, nil
	}
	buf := make([]byte, int(length))
	n, err := p.reader.ReadAt(buf, offset)
	if err != nil && !(errors.Is(err, io.EOF) && n > 0) {
		return nil, err
	}
	return buf[:n], nil
}

func parseShortAD(data []byte, part uint16) []isoExtent {
	extents := make([]isoExtent, 0)
	for i := 0; i+8 <= len(data); i += 8 {
		rawLength := le32(data[i : i+4])
		length := rawLength & 0x3fffffff
		location := le32(data[i+4 : i+8])
		if length > 0 {
			extents = append(extents, isoExtent{location: location, length: length, part: part})
		}
	}
	return extents
}

func parseLongAD(data []byte) []isoExtent {
	extents := make([]isoExtent, 0)
	for i := 0; i+16 <= len(data); i += 16 {
		ext := longAD(data[i : i+16])
		if ext.length > 0 {
			extents = append(extents, ext)
		}
	}
	return extents
}

func tagID(data []byte) uint16 {
	if len(data) < 2 {
		return 0
	}
	return le16(data[:2])
}

func extentAD(data []byte) isoExtent {
	return isoExtent{length: le32(data[:4]) & 0x3fffffff, location: le32(data[4:8])}
}

func longAD(data []byte) isoExtent {
	return isoExtent{
		length:   le32(data[:4]) & 0x3fffffff,
		location: le32(data[4:8]),
		part:     le16(data[8:10]),
	}
}

func decodeUDFName(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	compression := data[0]
	payload := data[1:]
	if compression == 16 {
		runes := make([]rune, 0, len(payload)/2)
		for i := 0; i+1 < len(payload); i += 2 {
			r := rune(payload[i])<<8 | rune(payload[i+1])
			if r != 0 {
				runes = append(runes, r)
			}
		}
		return strings.TrimSpace(string(runes))
	}
	return strings.TrimSpace(string(bytes.Trim(payload, "\x00")))
}

func joinISOPath(parent, name string) string {
	if parent == "" || parent == "/" {
		return "/" + name
	}
	return strings.TrimRight(parent, "/") + "/" + name
}

func align4(value int) int {
	if value%4 == 0 {
		return value
	}
	return value + 4 - value%4
}

func le16(data []byte) uint16 {
	return uint16(data[0]) | uint16(data[1])<<8
}

func le32(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func le64(data []byte) uint64 {
	return uint64(le32(data[:4])) | uint64(le32(data[4:8]))<<32
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

var invalidFFProbeURLCharRE = regexp.MustCompile(`[\r\n]`)

func CleanURL(value string) string {
	return invalidFFProbeURLCharRE.ReplaceAllString(strings.TrimSpace(value), "")
}
