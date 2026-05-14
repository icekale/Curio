package p115

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"curio/internal/models"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type TreeItem struct {
	RelativePath   string
	Name           string
	RemoteFileID   string
	PickCode       string
	SHA1           string
	Size           int64
	Depth          int
	IsDirectory    bool
	SourceTreeHash string
}

type FileInfo struct {
	ID          string
	Name        string
	PickCode    string
	SHA1        string
	Size        int64
	IsDirectory bool
}

type openQRCodeSession struct {
	UID       string
	QRCodeURL string
	Time      string
	Sign      string
}

type openTokenPair struct {
	AccessToken  string
	RefreshToken string
}

type Client struct {
	settings models.P115Settings
	http     *http.Client
}

func NewClient(settings models.P115Settings) *Client {
	return &Client{
		settings: settings,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) Status(ctx context.Context) (models.P115Status, error) {
	status := models.P115Status{
		Ready:     c.settings.Enabled,
		CanExport: c.hasOpenToken() || c.hasCookies(),
		CanPlay:   c.hasOpenToken() || c.hasCookies(),
	}
	if !c.settings.Enabled {
		status.Message = "115 播放未启用"
		return status, nil
	}
	if c.preferOpen() {
		payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://proapi.115.com/open/user/info", nil, true, "")
		if err != nil {
			return status, err
		}
		status.Ready = true
		status.UserName = firstString(payload, "user_name", "name", "nickname", "uid")
		status.Message = "115 Open 已连接"
		return status, nil
	}
	if c.hasCookies() {
		if _, err := c.List(ctx, "0"); err != nil {
			return status, err
		}
		status.Ready = true
		status.Message = "115 Cookies 已连接"
		return status, nil
	}
	status.Ready = false
	status.Message = "请配置 115 Cookies 或 Open Access Token"
	return status, nil
}

func (c *Client) StartOpenQRCode(ctx context.Context, codeChallenge string) (openQRCodeSession, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(c.settings.AppID))
	form.Set("code_challenge", codeChallenge)
	form.Set("code_challenge_method", "sha256")
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/authDeviceCode", form, false, "")
	if err != nil {
		return openQRCodeSession{}, err
	}
	if err := payloadError(payload); err != nil {
		return openQRCodeSession{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	uid := firstString(data, "uid", "qrcode_uid")
	if uid == "" {
		return openQRCodeSession{}, errors.New("115 未返回扫码会话 UID")
	}
	return openQRCodeSession{
		UID:       uid,
		QRCodeURL: "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=" + url.QueryEscape(uid),
		Time:      firstString(data, "time", "timestamp"),
		Sign:      firstString(data, "sign", "signature"),
	}, nil
}

func (c *Client) StartCookieQRCode(ctx context.Context) (openQRCodeSession, error) {
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://qrcodeapi.115.com/api/1.0/web/1.0/token/", nil, false, "")
	if err != nil {
		return openQRCodeSession{}, err
	}
	if err := payloadError(payload); err != nil {
		return openQRCodeSession{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	uid := firstString(data, "uid", "qrcode_uid")
	if uid == "" {
		return openQRCodeSession{}, errors.New("115 未返回扫码会话 UID")
	}
	return openQRCodeSession{
		UID:       uid,
		QRCodeURL: "https://qrcodeapi.115.com/api/1.0/web/1.0/qrcode?uid=" + url.QueryEscape(uid),
		Time:      firstString(data, "time", "timestamp"),
		Sign:      firstString(data, "sign", "signature"),
	}, nil
}

func (c *Client) OpenQRCodeStatus(ctx context.Context, uid, timeValue, sign string) (models.P115QRCodeStatus, error) {
	query := url.Values{}
	query.Set("uid", uid)
	if timeValue != "" {
		query.Set("time", timeValue)
	}
	if sign != "" {
		query.Set("sign", sign)
	}
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://qrcodeapi.115.com/get/status/", query, false, "")
	if err != nil {
		return models.P115QRCodeStatus{}, err
	}
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	status := firstString(data, "status", "code", "state")
	return models.P115QRCodeStatus{
		UID:     uid,
		Status:  status,
		Message: qrStatusMessage(status, responseMessage(payload)),
	}, nil
}

func (c *Client) CookieQRCodeToCookies(ctx context.Context, uid, app string) (string, error) {
	app = NormalizeCookieLoginApp(app)
	form := url.Values{}
	form.Set("account", strings.TrimSpace(uid))
	form.Set("app", app)
	var lastErr error
	for _, endpoint := range []string{
		"https://passportapi.115.com/app/1.0/" + app + "/1.0/login/qrcode/",
		"https://qrcodeapi.115.com/app/1.0/" + app + "/1.0/login/qrcode/",
	} {
		payload, header, err := c.requestJSON(ctx, http.MethodPost, endpoint, form, false, "")
		if err != nil {
			lastErr = err
			continue
		}
		if err := payloadError(payload); err != nil {
			lastErr = err
			continue
		}
		cookies := cookiesFromLoginResponse(payload, header)
		if cookies != "" {
			return cookies, nil
		}
		lastErr = errors.New("115 未返回 Cookies，请确认已在手机端完成扫码")
	}
	return "", lastErr
}

func (c *Client) OpenDeviceCodeToToken(ctx context.Context, uid, codeVerifier string) (openTokenPair, error) {
	form := url.Values{}
	form.Set("uid", uid)
	form.Set("code_verifier", codeVerifier)
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/deviceCodeToToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) OpenOAuthAuthorizeURL(redirectURI, state string) models.P115OAuthStart {
	values := url.Values{}
	values.Set("client_id", strings.TrimSpace(c.settings.AppID))
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("state", state)
	return models.P115OAuthStart{
		AuthorizeURL: "https://qrcodeapi.115.com/open/authorize?" + values.Encode(),
		RedirectURI:  redirectURI,
		State:        state,
	}
}

func (c *Client) OpenAuthCodeToToken(ctx context.Context, code, redirectURI string) (openTokenPair, error) {
	if strings.TrimSpace(c.settings.AppSecret) == "" {
		return openTokenPair{}, errors.New("请先填写 115 App Secret")
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(c.settings.AppID))
	form.Set("client_secret", strings.TrimSpace(c.settings.AppSecret))
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/authCodeToToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) RefreshOpenToken(ctx context.Context) (openTokenPair, error) {
	form := url.Values{}
	form.Set("refresh_token", strings.TrimSpace(c.settings.RefreshToken))
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://qrcodeapi.115.com/open/refreshToken", form, false, "")
	if err != nil {
		return openTokenPair{}, err
	}
	if err := payloadError(payload); err != nil {
		return openTokenPair{}, err
	}
	return tokenPairFromPayload(payload)
}

func (c *Client) hasCookies() bool {
	return strings.TrimSpace(c.settings.Cookies) != ""
}

func (c *Client) hasOpenToken() bool {
	return strings.TrimSpace(c.settings.AccessToken) != ""
}

func (c *Client) preferOpen() bool {
	if !c.hasOpenToken() {
		return false
	}
	if !c.hasCookies() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(c.settings.AuthMode), authModeOpen)
}

func (c *Client) ExportTree(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	if c.hasCookies() {
		items, err := c.exportTreeByWeb(ctx, lib)
		if err == nil {
			return items, nil
		}
		webListItems, listErr := c.exportTreeByWebList(ctx, lib)
		if listErr == nil {
			return webListItems, nil
		}
		if !c.hasOpenToken() {
			return nil, fmt.Errorf("%w；Cookie 分页兜底也失败：%v", err, listErr)
		}
	}
	return c.exportTreeByList(ctx, lib)
}

func (c *Client) ResolvePath(ctx context.Context, cid, relativePath string) (FileInfo, error) {
	segments := splitRelativePath(relativePath)
	if len(segments) == 0 {
		return FileInfo{}, errors.New("115 相对路径为空")
	}
	current := strings.TrimSpace(cid)
	for index, name := range segments {
		children, err := c.List(ctx, current)
		if err != nil {
			return FileInfo{}, err
		}
		var match *FileInfo
		for i := range children {
			if children[i].Name == name {
				match = &children[i]
				break
			}
		}
		if match == nil {
			return FileInfo{}, fmt.Errorf("115 路径不存在：%s", relativePath)
		}
		if index == len(segments)-1 {
			if match.IsDirectory {
				return FileInfo{}, fmt.Errorf("115 路径不是文件：%s", relativePath)
			}
			return *match, nil
		}
		if !match.IsDirectory {
			return FileInfo{}, fmt.Errorf("115 路径中间节点不是目录：%s", name)
		}
		current = match.ID
	}
	return FileInfo{}, fmt.Errorf("115 路径不存在：%s", relativePath)
}

func (c *Client) List(ctx context.Context, cid string) ([]FileInfo, error) {
	if c.preferOpen() {
		return c.listOpen(ctx, cid)
	}
	if c.hasCookies() {
		return c.listWeb(ctx, cid)
	}
	if c.hasOpenToken() {
		return c.listOpen(ctx, cid)
	}
	return nil, errors.New("115 未配置可用授权")
}

func (c *Client) DirectURL(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	pickcode = strings.TrimSpace(pickcode)
	if pickcode == "" {
		return "", errors.New("115 pickcode 为空")
	}
	if c.preferOpen() {
		directURL, err := c.directURLOpen(ctx, pickcode, userAgentValue)
		if err == nil {
			return directURL, nil
		}
		if c.hasCookies() {
			if fallbackURL, fallbackErr := c.directURLWeb(ctx, pickcode, userAgentValue); fallbackErr == nil {
				return fallbackURL, nil
			} else {
				return "", fmt.Errorf("Open 直链失败：%v；Cookies 直链也失败：%v", err, fallbackErr)
			}
		}
		return "", err
	}
	if c.hasCookies() {
		directURL, err := c.directURLWeb(ctx, pickcode, userAgentValue)
		if err == nil {
			return directURL, nil
		}
		if c.hasOpenToken() {
			if fallbackURL, fallbackErr := c.directURLOpen(ctx, pickcode, userAgentValue); fallbackErr == nil {
				return fallbackURL, nil
			} else {
				return "", fmt.Errorf("Cookies 直链失败：%v；Open 直链也失败：%v", err, fallbackErr)
			}
		}
		return "", err
	}
	if c.hasOpenToken() {
		return c.directURLOpen(ctx, pickcode, userAgentValue)
	}
	return "", errors.New("115 未配置可用授权")
}

func (c *Client) exportTreeByWeb(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	form := url.Values{}
	form.Set("file_ids", lib.CID)
	form.Set("target", "U_0_0")
	if lib.LayerLimit > 0 {
		form.Set("layer_limit", strconv.Itoa(lib.LayerLimit))
	}
	resp, _, err := c.requestJSON(ctx, http.MethodPost, "https://webapi.115.com/files/export_dir", form, false, "")
	if err != nil {
		return nil, err
	}
	data := asMap(resp["data"])
	exportID := firstString(data, "export_id", "id")
	if exportID == "" {
		return nil, errors.New("115 未返回目录树导出任务 ID")
	}
	var result map[string]any
	deadline := time.Now().Add(10 * time.Minute)
	for {
		query := url.Values{}
		query.Set("export_id", exportID)
		payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://webapi.115.com/files/export_dir", query, false, "")
		if err != nil {
			return nil, err
		}
		if data := asMap(payload["data"]); len(data) > 0 {
			result = data
			break
		}
		if time.Now().After(deadline) {
			return nil, errors.New("115 目录树导出超时")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	pickcode := firstString(result, "pick_code", "pickcode", "pc")
	if pickcode == "" {
		return nil, errors.New("115 目录树导出结果缺少 pickcode")
	}
	downloadURL, err := c.directURLWeb(ctx, pickcode, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	body, err := c.download(ctx, downloadURL, defaultUserAgent)
	if err != nil {
		return nil, err
	}
	if fileID := firstString(result, "file_id", "fid", "id"); fileID != "" {
		_ = c.deleteWeb(ctx, fileID)
	}
	return parseExportTree(body)
}

func (c *Client) exportTreeByList(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	return c.exportTreeByListWith(ctx, lib, c.List)
}

func (c *Client) exportTreeByWebList(ctx context.Context, lib LibraryConfig) ([]TreeItem, error) {
	return c.exportTreeByListWith(ctx, lib, c.listWeb)
}

func (c *Client) exportTreeByListWith(ctx context.Context, lib LibraryConfig, list func(context.Context, string) ([]FileInfo, error)) ([]TreeItem, error) {
	items := make([]TreeItem, 0)
	err := c.walk(ctx, lib.CID, "", 0, lib.LayerLimit, list, &items)
	return items, err
}

func (c *Client) walk(ctx context.Context, cid, prefix string, depth, limit int, list func(context.Context, string) ([]FileInfo, error), items *[]TreeItem) error {
	if limit > 0 && depth >= limit {
		return nil
	}
	children, err := list(ctx, cid)
	if err != nil {
		return err
	}
	for _, child := range children {
		rel := child.Name
		if prefix != "" {
			rel = prefix + "/" + child.Name
		}
		item := TreeItem{
			RelativePath:   rel,
			Name:           child.Name,
			RemoteFileID:   child.ID,
			PickCode:       child.PickCode,
			SHA1:           child.SHA1,
			Size:           child.Size,
			Depth:          depth + 1,
			IsDirectory:    child.IsDirectory,
			SourceTreeHash: sourceHash(cid, rel, child.ID, child.PickCode, child.SHA1, child.Size),
		}
		*items = append(*items, item)
		if child.IsDirectory {
			if err := c.walk(ctx, child.ID, rel, depth+1, limit, list, items); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) listOpen(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://proapi.115.com/open/ufile/files", cid, true)
}

func (c *Client) listWeb(ctx context.Context, cid string) ([]FileInfo, error) {
	return c.listPaged(ctx, "https://webapi.115.com/files", cid, false)
}

func (c *Client) listPaged(ctx context.Context, endpoint, cid string, open bool) ([]FileInfo, error) {
	out := make([]FileInfo, 0)
	for offset := 0; ; offset += 1150 {
		query := url.Values{}
		query.Set("cid", strings.TrimSpace(cid))
		query.Set("limit", "1150")
		query.Set("offset", strconv.Itoa(offset))
		query.Set("show_dir", "1")
		query.Set("count_folders", "1")
		query.Set("natsort", "1")
		query.Set("fc_mix", "0")
		payload, _, err := c.requestJSON(ctx, http.MethodGet, endpoint, query, open, "")
		if err != nil {
			return nil, err
		}
		rows := extractArray(payload)
		for _, row := range rows {
			info := fileInfoFromMap(row)
			if info.Name != "" {
				out = append(out, info)
			}
		}
		if len(rows) < 1150 {
			break
		}
	}
	return out, nil
}

func (c *Client) directURLOpen(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	form := url.Values{}
	form.Set("pick_code", pickcode)
	payload, _, err := c.requestJSON(ctx, http.MethodPost, "https://proapi.115.com/open/ufile/downurl", form, true, userAgentValue)
	if err != nil {
		return "", err
	}
	if value := extractDownloadURL(payload); value != "" {
		return value, nil
	}
	return "", errors.New("115 Open 未返回下载直链")
}

func (c *Client) directURLWeb(ctx context.Context, pickcode, userAgentValue string) (string, error) {
	query := url.Values{}
	query.Set("pickcode", pickcode)
	query.Set("dl", "1")
	payload, _, err := c.requestJSON(ctx, http.MethodGet, "https://webapi.115.com/files/download", query, false, userAgentValue)
	if err == nil {
		if value := extractDownloadURL(payload); value != "" {
			return value, nil
		}
	}
	raw := "https://115.com/?" + url.Values{"ct": []string{"download"}, "ac": []string{"video"}, "pickcode": []string{pickcode}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	c.decorate(req, false, userAgentValue)
	client := *c.http
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if location := strings.TrimSpace(resp.Header.Get("Location")); location != "" {
			return location, nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", errors.New("Cookies 模式未能解析 115 直链；大文件播放建议配置 Open Access Token")
}

func (c *Client) requestJSON(ctx context.Context, method, rawURL string, values url.Values, open bool, userAgentValue string) (map[string]any, http.Header, error) {
	reqURL := rawURL
	var body io.Reader
	if method == http.MethodGet {
		if len(values) > 0 {
			sep := "?"
			if strings.Contains(reqURL, "?") {
				sep = "&"
			}
			reqURL += sep + values.Encode()
		}
	} else if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, nil, err
	}
	c.decorate(req, open, userAgentValue)
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("115 请求失败：HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, resp.Header, err
	}
	if !responseOK(payload) {
		return nil, resp.Header, fmt.Errorf("115 请求失败：%s", responseMessage(payload))
	}
	return payload, resp.Header, nil
}

func (c *Client) decorate(req *http.Request, open bool, userAgentValue string) {
	req.Header.Set("User-Agent", userAgent(c.settings, userAgentValue))
	if open {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.settings.AccessToken))
		return
	}
	if cookies := strings.TrimSpace(c.settings.Cookies); cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
}

func (c *Client) download(ctx context.Context, rawURL, userAgentValue string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req, false, userAgentValue)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("115 下载目录树失败：HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

func (c *Client) deleteWeb(ctx context.Context, fileID string) error {
	form := url.Values{}
	form.Set("fid", fileID)
	_, _, err := c.requestJSON(ctx, http.MethodPost, "https://webapi.115.com/rb/delete", form, false, "")
	return err
}

func parseExportTree(data []byte) ([]TreeItem, error) {
	decoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	decoded, _, err := transform.Bytes(decoder, data)
	if err != nil {
		decoded = data
	}
	text := strings.ReplaceAll(string(decoded), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	stack := map[int]string{}
	items := make([]TreeItem, 0)
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		idx := strings.LastIndex(line, "|-")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line[idx+2:], "-"))
		if name == "" {
			continue
		}
		depth := strings.Count(line[:idx], "|") + 1
		stack[depth] = name
		for key := range stack {
			if key > depth {
				delete(stack, key)
			}
		}
		parts := make([]string, 0, depth)
		for i := 1; i <= depth; i++ {
			if stack[i] != "" {
				parts = append(parts, stack[i])
			}
		}
		rel := path.Join(parts...)
		ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
		items = append(items, TreeItem{
			RelativePath:   rel,
			Name:           name,
			Depth:          depth,
			IsDirectory:    ext == "",
			SourceTreeHash: sourceHash("", rel, "", "", "", 0),
		})
	}
	return items, nil
}

func splitRelativePath(value string) []string {
	value = strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

func fileInfoFromMap(row map[string]any) FileInfo {
	info := FileInfo{
		ID:       firstString(row, "file_id", "fid", "id", "cid"),
		Name:     firstString(row, "file_name", "name", "n", "fn"),
		PickCode: firstString(row, "pick_code", "pickcode", "pc"),
		SHA1:     firstString(row, "sha1", "sha", "file_sha1"),
		Size:     firstInt64(row, "size", "s", "file_size", "fs"),
	}
	info.IsDirectory = firstBool(row, "is_dir", "is_directory", "is_folder")
	if !hasAny(row, "is_dir", "is_directory", "is_folder") {
		if fc := firstString(row, "fc"); fc != "" {
			info.IsDirectory = fc == "0"
		}
	}
	if !info.IsDirectory && info.PickCode == "" && firstString(row, "cid") != "" && firstString(row, "fid", "file_id") == "" {
		info.IsDirectory = true
	}
	return info
}

func responseOK(payload map[string]any) bool {
	value, ok := payload["state"]
	if !ok {
		return true
	}
	switch v := value.(type) {
	case bool:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n != 0
	case float64:
		return v != 0
	case string:
		return v != "0" && !strings.EqualFold(v, "false")
	default:
		return true
	}
}

func responseMessage(payload map[string]any) string {
	if msg := firstString(payload, "error", "error_msg", "message", "msg"); msg != "" {
		return msg
	}
	if code := firstString(payload, "errno", "errNo", "errcode", "code"); code != "" {
		return code
	}
	return "未知错误"
}

func payloadError(payload map[string]any) error {
	if !responseOK(payload) {
		return fmt.Errorf("115 请求失败：%s", responseMessage(payload))
	}
	for _, key := range []string{"code", "errno", "errNo", "errcode"} {
		code := firstString(payload, key)
		if code == "" || code == "0" || strings.EqualFold(code, "success") {
			continue
		}
		return fmt.Errorf("115 请求失败：%s", responseMessage(payload))
	}
	return nil
}

func tokenPairFromPayload(payload map[string]any) (openTokenPair, error) {
	data := asMap(payload["data"])
	if len(data) == 0 {
		data = payload
	}
	token := openTokenPair{
		AccessToken:  firstString(data, "access_token", "accessToken"),
		RefreshToken: firstString(data, "refresh_token", "refreshToken"),
	}
	if token.AccessToken == "" {
		return openTokenPair{}, errors.New("115 未返回 Access Token")
	}
	return token, nil
}

func cookiesFromLoginResponse(payload map[string]any, header http.Header) string {
	for _, row := range []map[string]any{asMap(payload["data"]), payload} {
		if len(row) == 0 {
			continue
		}
		for _, key := range []string{"cookie", "cookies"} {
			if cookies := cookiesFromValue(row[key]); cookies != "" {
				return cookies
			}
		}
	}
	response := http.Response{Header: header}
	return cookiesFromHTTP(response.Cookies())
}

func cookiesFromValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, key := range []string{"UID", "CID", "SEID", "KID"} {
			if value := firstString(v, key, strings.ToLower(key)); value != "" {
				parts = append(parts, key+"="+value)
			}
		}
		for key, raw := range v {
			if hasCookieKey(parts, key) {
				continue
			}
			if value := cookieValue(raw); value != "" {
				parts = append(parts, key+"="+value)
			}
		}
		return strings.Join(parts, "; ")
	case []any:
		cookies := make([]*http.Cookie, 0, len(v))
		for _, item := range v {
			row := asMap(item)
			if len(row) == 0 {
				continue
			}
			name := firstString(row, "name", "Name")
			value := firstString(row, "value", "Value")
			if name != "" && value != "" {
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
		}
		return cookiesFromHTTP(cookies)
	default:
		return ""
	}
}

func cookiesFromHTTP(cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" || strings.TrimSpace(cookie.Value) == "" {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func cookieValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(strings.Trim(v, "\""))
	case json.Number:
		return v.String()
	case float64:
		if v != 0 {
			return strconv.FormatInt(int64(v), 10)
		}
	case int64:
		if v != 0 {
			return strconv.FormatInt(v, 10)
		}
	case int:
		if v != 0 {
			return strconv.Itoa(v)
		}
	}
	return ""
}

func hasCookieKey(parts []string, key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, part := range parts {
		name, _, _ := strings.Cut(part, "=")
		if strings.ToLower(strings.TrimSpace(name)) == key {
			return true
		}
	}
	return false
}

func qrStatusMessage(status, fallback string) string {
	switch status {
	case "0":
		return "等待扫码"
	case "1":
		return "已扫码，请在手机端确认"
	case "2":
		return "已确认，可以完成登录"
	case "-1", "-2":
		return "二维码已过期，请重新生成"
	}
	if fallback != "" && fallback != "未知错误" {
		return fallback
	}
	if status != "" {
		return "扫码状态：" + status
	}
	return "等待扫码"
}

func extractArray(payload map[string]any) []map[string]any {
	for _, key := range []string{"data", "list", "items", "files"} {
		switch value := payload[key].(type) {
		case []any:
			return mapsFromArray(value)
		case map[string]any:
			if rows := extractArray(value); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func mapsFromArray(values []any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if row := asMap(value); len(row) > 0 {
			out = append(out, row)
		}
	}
	return out
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if row, ok := value.(map[string]any); ok {
		return row
	}
	return nil
}

func extractDownloadURL(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"url", "file_url", "download_url", "downurl"} {
			if raw := firstString(v, key); strings.HasPrefix(raw, "http") {
				return raw
			}
			if nested := asMap(v[key]); len(nested) > 0 {
				if raw := firstString(nested, "url", "file_url", "download_url"); strings.HasPrefix(raw, "http") {
					return raw
				}
			}
		}
		for _, child := range v {
			if raw := extractDownloadURL(child); raw != "" {
				return raw
			}
		}
	case []any:
		for _, child := range v {
			if raw := extractDownloadURL(child); raw != "" {
				return raw
			}
		}
	}
	return ""
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			switch v := value.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			case json.Number:
				return v.String()
			case float64:
				if v != 0 {
					return strconv.FormatInt(int64(v), 10)
				}
			case int64:
				if v != 0 {
					return strconv.FormatInt(v, 10)
				}
			case int:
				if v != 0 {
					return strconv.Itoa(v)
				}
			}
		}
	}
	return ""
}

func firstInt64(row map[string]any, keys ...string) int64 {
	raw := firstString(row, keys...)
	if raw == "" {
		return 0
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func firstBool(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch v := row[key].(type) {
		case bool:
			return v
		case json.Number:
			n, _ := v.Int64()
			return n != 0
		case float64:
			return v != 0
		case string:
			return v == "1" || strings.EqualFold(v, "true")
		}
	}
	return false
}

func hasAny(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := row[key]; ok {
			return true
		}
	}
	return false
}
