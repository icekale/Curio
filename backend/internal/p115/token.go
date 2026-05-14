package p115

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

const tokenType = "p115_play"

type playTokenPayload struct {
	Type   string `json:"type"`
	LinkID string `json:"link_id"`
	Exp    int64  `json:"exp"`
}

func signPlayToken(linkID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 10 * 365 * 24 * time.Hour
	}
	payload := playTokenPayload{Type: tokenType, LinkID: linkID, Exp: time.Now().Add(ttl).Unix()}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(playSecret()))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyPlayToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("播放 token 无效")
	}
	mac := hmac.New(sha256.New, []byte(playSecret()))
	_, _ = mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(actual, expected) {
		return "", errors.New("播放 token 签名无效")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("播放 token 内容无效")
	}
	var payload playTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.Type != tokenType || strings.TrimSpace(payload.LinkID) == "" {
		return "", errors.New("播放 token 类型无效")
	}
	if payload.Exp > 0 && time.Now().Unix() > payload.Exp {
		return "", errors.New("播放 token 已过期")
	}
	return payload.LinkID, nil
}

func playSecret() string {
	if value := strings.TrimSpace(os.Getenv("CURIO_PLAY_SECRET")); value != "" {
		return value
	}
	return "curio-change-me"
}
