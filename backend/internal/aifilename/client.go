package aifilename

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"curio/internal/netproxy"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-5.5"
)

type Settings struct {
	Enabled      bool
	Force        bool
	BaseURL      string
	APIKey       string
	Model        string
	Prompt       string
	NetworkProxy string
}

type File struct {
	Index     int    `json:"index"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
}

type Analysis struct {
	Index             int
	MediaType         string
	Title             string
	AlternativeTitles []string
	Year              int
	Season            int
	Episode           int
	EpisodeEnd        int
	Resolution        string
	Source            string
	VideoCodec        string
	AudioCodec        string
	AudioChannels     string
	HDRFormat         string
	Edition           string
	ReleaseGroup      string
	Confidence        float64
	NeedsReview       bool
	Reason            string
}

type AnalyzeResult struct {
	Items []Analysis
	Log   CallLog
}

type CallLog struct {
	Model          string
	BaseURL        string
	Endpoint       string
	ProxyURL       string
	ResponseFormat string
	RequestJSON    string
	ResponseJSON   string
	HTTPStatus     int
	DurationMS     int64
	Attempt        int
	ErrorMessage   string
}

type Client struct {
	settings Settings
	http     *http.Client
}

func New(settings Settings) (*Client, error) {
	settings.BaseURL = strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if settings.BaseURL == "" {
		settings.BaseURL = defaultBaseURL
	}
	settings.Model = strings.TrimSpace(settings.Model)
	if settings.Model == "" {
		settings.Model = defaultModel
	}
	httpClient, err := httpClient(settings.NetworkProxy)
	if err != nil {
		return nil, err
	}
	return &Client{settings: settings, http: httpClient}, nil
}

func (c *Client) Analyze(ctx context.Context, files []File) ([]Analysis, error) {
	result, err := c.AnalyzeDetailed(ctx, files)
	return result.Items, err
}

func (c *Client) AnalyzeDetailed(ctx context.Context, files []File) (AnalyzeResult, error) {
	if c == nil {
		return AnalyzeResult{}, errors.New("AI 文件名识别未初始化")
	}
	if len(files) == 0 {
		return AnalyzeResult{}, nil
	}
	formats := []struct {
		name   string
		format any
	}{
		{name: "json_schema", format: jsonSchemaResponseFormat()},
		{name: "json_object", format: map[string]any{"type": "json_object"}},
		{name: "plain_json", format: nil},
	}
	var last AnalyzeResult
	for index, format := range formats {
		request := c.chatRequest(files, format.format)
		call, err := c.createChatCompletion(ctx, request)
		call.Log.Attempt = index + 1
		call.Log.ResponseFormat = format.name
		last.Log = call.Log
		if err != nil {
			last.Log.ErrorMessage = err.Error()
			if shouldRetryWithoutSchema(err) && index < len(formats)-1 {
				continue
			}
			return last, err
		}
		items, err := decodeAnalysisPayload([]byte(call.Content))
		if err != nil {
			last.Log.ErrorMessage = err.Error()
			return last, err
		}
		last.Items = items
		return last, nil
	}
	if last.Log.ErrorMessage != "" {
		return last, errors.New(last.Log.ErrorMessage)
	}
	return last, errors.New("AI 文件名识别失败")
}

func (c *Client) chatRequest(files []File, responseFormat any) chatCompletionRequest {
	prompt := strings.TrimSpace(c.settings.Prompt)
	if prompt == "" {
		prompt = "Analyze media file names and return only JSON."
	}
	body := map[string]any{
		"instructions": "Return only JSON with an items array. Keep each input index unchanged.",
		"files":        files,
	}
	raw, _ := json.Marshal(body)
	messages := []chatMessage{
		{Role: "system", Content: prompt},
		{Role: "user", Content: string(raw)},
	}
	return chatCompletionRequest{
		Model:          c.settings.Model,
		Messages:       messages,
		ResponseFormat: responseFormat,
	}
}

type chatCompletionResult struct {
	Content string
	Log     CallLog
}

func (c *Client) createChatCompletion(ctx context.Context, payload chatCompletionRequest) (chatCompletionResult, error) {
	raw, err := json.Marshal(payload)
	logEntry := CallLog{
		Model:       c.settings.Model,
		BaseURL:     c.settings.BaseURL,
		Endpoint:    chatCompletionsURL(c.settings.BaseURL),
		ProxyURL:    strings.TrimSpace(c.settings.NetworkProxy),
		RequestJSON: string(raw),
	}
	if err != nil {
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, logEntry.Endpoint, bytes.NewReader(raw))
	if err != nil {
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.settings.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.settings.APIKey))
	}
	started := time.Now()
	resp, err := c.http.Do(req)
	logEntry.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	defer resp.Body.Close()
	logEntry.HTTPStatus = resp.StatusCode
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	logEntry.ResponseJSON = string(body)
	if readErr != nil {
		logEntry.ErrorMessage = readErr.Error()
		return chatCompletionResult{Log: logEntry}, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := apiStatusError{StatusCode: resp.StatusCode, Message: apiErrorMessage(body)}
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	var decoded chatCompletionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	if len(decoded.Choices) == 0 {
		err := errors.New("AI 接口没有返回 choices")
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		err := errors.New("AI 接口返回内容为空")
		logEntry.ErrorMessage = err.Error()
		return chatCompletionResult{Log: logEntry}, err
	}
	return chatCompletionResult{Content: content, Log: logEntry}, nil
}

type chatCompletionRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    *float64      `json:"temperature,omitempty"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type apiStatusError struct {
	StatusCode int
	Message    string
}

func (e apiStatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("AI 接口返回状态码 %d", e.StatusCode)
	}
	return fmt.Sprintf("AI 接口返回状态码 %d: %s", e.StatusCode, e.Message)
}

func shouldRetryWithoutSchema(err error) bool {
	var status apiStatusError
	if !errors.As(err, &status) {
		return false
	}
	return status.StatusCode == http.StatusBadRequest || status.StatusCode == http.StatusUnprocessableEntity
}

func apiErrorMessage(body []byte) string {
	var payload struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		switch value := payload.Error.(type) {
		case string:
			return value
		case map[string]any:
			if message, ok := value["message"].(string); ok {
				return message
			}
		}
	}
	return strings.TrimSpace(string(body))
}

func chatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	if strings.HasSuffix(lower, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}

func httpClient(proxyURL string) (*http.Client, error) {
	return netproxy.HTTPClient(proxyURL, 60*time.Second)
}

func jsonSchemaResponseFormat() map[string]any {
	stringOrNull := []any{"string", "null"}
	intOrNull := []any{"integer", "null"}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "curio_filename_analysis",
			"strict": true,
			"schema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"items"},
				"properties": map[string]any{
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required": []string{
								"index", "media_type", "title", "alternative_titles", "year", "season", "episode", "episode_end",
								"resolution", "source", "video_codec", "audio_codec", "audio_channels", "hdr_format",
								"edition", "release_group", "confidence", "needs_review", "reason",
							},
							"properties": map[string]any{
								"index":              map[string]any{"type": "integer"},
								"media_type":         map[string]any{"type": "string", "enum": []string{"movie", "tv_episode", "unknown"}},
								"title":              map[string]any{"type": stringOrNull},
								"alternative_titles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"year":               map[string]any{"type": intOrNull},
								"season":             map[string]any{"type": intOrNull},
								"episode":            map[string]any{"type": intOrNull},
								"episode_end":        map[string]any{"type": intOrNull},
								"resolution":         map[string]any{"type": stringOrNull},
								"source":             map[string]any{"type": stringOrNull},
								"video_codec":        map[string]any{"type": stringOrNull},
								"audio_codec":        map[string]any{"type": stringOrNull},
								"audio_channels":     map[string]any{"type": stringOrNull},
								"hdr_format":         map[string]any{"type": stringOrNull},
								"edition":            map[string]any{"type": stringOrNull},
								"release_group":      map[string]any{"type": stringOrNull},
								"confidence":         map[string]any{"type": "number"},
								"needs_review":       map[string]any{"type": "boolean"},
								"reason":             map[string]any{"type": stringOrNull},
							},
						},
					},
				},
			},
		},
	}
}

func decodeAnalysisPayload(data []byte) ([]Analysis, error) {
	data = []byte(extractJSONObject(strings.TrimSpace(string(data))))
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		var rawItems []map[string]any
		decoder = json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if arrayErr := decoder.Decode(&rawItems); arrayErr != nil {
			return nil, err
		}
		payload.Items = rawItems
	}
	items := make([]Analysis, 0, len(payload.Items))
	for _, raw := range payload.Items {
		item := Analysis{
			Index:             looseInt(raw["index"]),
			MediaType:         normalizeMediaType(looseString(raw["media_type"])),
			Title:             looseString(raw["title"]),
			AlternativeTitles: looseStringSlice(raw["alternative_titles"]),
			Year:              looseInt(raw["year"]),
			Season:            looseInt(raw["season"]),
			Episode:           looseInt(raw["episode"]),
			EpisodeEnd:        looseInt(raw["episode_end"]),
			Resolution:        looseString(raw["resolution"]),
			Source:            looseString(raw["source"]),
			VideoCodec:        looseString(raw["video_codec"]),
			AudioCodec:        looseString(raw["audio_codec"]),
			AudioChannels:     looseString(raw["audio_channels"]),
			HDRFormat:         looseString(raw["hdr_format"]),
			Edition:           looseString(raw["edition"]),
			ReleaseGroup:      looseString(raw["release_group"]),
			Confidence:        looseFloat(raw["confidence"]),
			NeedsReview:       looseBool(raw["needs_review"]),
			Reason:            looseString(raw["reason"]),
		}
		if item.MediaType == "" {
			item.MediaType = "unknown"
		}
		items = append(items, item)
	}
	return items, nil
}

func extractJSONObject(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "[") {
		return content
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return content
}

func normalizeMediaType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movie", "film":
		return "movie"
	case "tv", "tv_episode", "episode", "series":
		return "tv_episode"
	default:
		return "unknown"
	}
}

func looseString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func looseInt(value any) int {
	switch typed := value.(type) {
	case json.Number:
		if number, err := typed.Int64(); err == nil {
			return int(number)
		}
	case float64:
		return int(typed)
	case string:
		number, _ := strconv.Atoi(strings.TrimSpace(typed))
		return number
	}
	return 0
}

func looseFloat(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		number, _ := typed.Float64()
		return number
	case float64:
		return typed
	case string:
		number, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return number
	}
	return 0
}

func looseBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func looseStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if single := looseString(value); single != "" {
			return []string{single}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		text := looseString(item)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	return out
}
