package api

import (
	"net/http/httptest"
	"testing"

	"curio/internal/models"
)

func TestPublicSettingsRedactSensitiveValues(t *testing.T) {
	system := publicSystemSettings(models.SystemSettings{
		TMDBAPIKey:        "tmdb-key",
		NetworkProxy:      "http://user:pass@127.0.0.1:7890",
		AIBaseURL:         "https://api.example.test/v1",
		AIAPIKey:          "ai-key",
		CloudDriveAddress: "http://127.0.0.1:19798",
		CloudDriveToken:   "cloud-token",
	})
	if system.TMDBAPIKey != hiddenSecretValue || system.NetworkProxy != hiddenSecretValue || system.AIBaseURL != hiddenSecretValue || system.AIAPIKey != hiddenSecretValue {
		t.Fatalf("system settings were not redacted: %#v", system)
	}
	if system.CloudDriveAddress != hiddenSecretValue || system.CloudDriveToken != hiddenSecretValue {
		t.Fatalf("system clouddrive settings were not redacted: %#v", system)
	}

	cloud := publicCloudDriveSettings(models.CloudDriveSettings{
		Address: "http://127.0.0.1:19798",
		Token:   "cloud-token",
	})
	if cloud.Address != hiddenSecretValue || cloud.Token != hiddenSecretValue {
		t.Fatalf("cloud settings were not redacted: %#v", cloud)
	}

	p115Settings := publicP115Settings(models.P115Settings{
		AppSecret:       "secret",
		Cookies:         "UID=1",
		PublicBaseURL:   "https://curio.example.test",
		EmbyUpstreamURL: "http://emby:8096",
		EmbyAPIKey:      "emby-key",
	})
	if p115Settings.AppSecret != hiddenSecretValue || p115Settings.Cookies != hiddenSecretValue || p115Settings.PublicBaseURL != hiddenSecretValue || p115Settings.EmbyUpstreamURL != hiddenSecretValue || p115Settings.EmbyAPIKey != hiddenSecretValue {
		t.Fatalf("p115 settings were not redacted: %#v", p115Settings)
	}
}

func TestPublicPreviewAndLogsRedactDomains(t *testing.T) {
	preview := publicSTRMPreview(models.STRMPreview{Items: []models.STRMPreviewItem{{
		PlayPath: "https://curio.example.test/play/115/id/file?x=1",
	}}})
	if preview.Items[0].PlayPath != "/play/115/id/file?x=1" {
		t.Fatalf("preview play path kept origin: %#v", preview.Items[0].PlayPath)
	}

	entry := publicLogEntry(models.LogEntry{
		BaseURL:  "https://api.example.test/v1",
		ProxyURL: "http://user:pass@127.0.0.1:7890",
	})
	if entry.BaseURL != hiddenSecretValue || entry.ProxyURL != hiddenSecretValue {
		t.Fatalf("log entry endpoint data was not redacted: %#v", entry)
	}
}

func TestMergeHiddenSettingsKeepsExistingValues(t *testing.T) {
	system := models.SystemSettings{
		TMDBAPIKey:   hiddenSecretValue,
		NetworkProxy: hiddenSecretValue,
		AIBaseURL:    hiddenSecretValue,
		AIAPIKey:     hiddenSecretValue,
	}
	mergeHiddenSystemSettings(&system, models.SystemSettings{
		TMDBAPIKey:   "tmdb-key",
		NetworkProxy: "http://127.0.0.1:7890",
		AIBaseURL:    "https://api.example.test/v1",
		AIAPIKey:     "ai-key",
	})
	if system.TMDBAPIKey != "tmdb-key" || system.NetworkProxy != "http://127.0.0.1:7890" || system.AIBaseURL != "https://api.example.test/v1" || system.AIAPIKey != "ai-key" {
		t.Fatalf("system hidden values were not preserved: %#v", system)
	}

	cloud := models.CloudDriveSettings{Address: hiddenSecretValue, Username: hiddenSecretValue, Password: hiddenSecretValue, Token: hiddenSecretValue}
	mergeHiddenCloudDriveSettings(&cloud, models.CloudDriveSettings{Address: "http://127.0.0.1:19798", Username: "user", Password: "pass", Token: "token"})
	if cloud.Address != "http://127.0.0.1:19798" || cloud.Username != "user" || cloud.Password != "pass" || cloud.Token != "token" {
		t.Fatalf("cloud hidden values were not preserved: %#v", cloud)
	}

	p115Settings := models.P115Settings{AppSecret: hiddenSecretValue, Cookies: hiddenSecretValue, PublicBaseURL: hiddenSecretValue, EmbyUpstreamURL: hiddenSecretValue, EmbyAPIKey: hiddenSecretValue}
	mergeHiddenP115Settings(&p115Settings, models.P115Settings{AppSecret: "secret", Cookies: "UID=1", PublicBaseURL: "https://curio.example.test", EmbyUpstreamURL: "http://emby:8096", EmbyAPIKey: "emby-key"})
	if p115Settings.AppSecret != "secret" || p115Settings.Cookies != "UID=1" || p115Settings.PublicBaseURL != "https://curio.example.test" || p115Settings.EmbyUpstreamURL != "http://emby:8096" || p115Settings.EmbyAPIKey != "emby-key" {
		t.Fatalf("p115 hidden values were not preserved: %#v", p115Settings)
	}
}

func TestAdminCookieTokenRoundTrip(t *testing.T) {
	req := httptest.NewRequest("GET", "https://curio.example.test/api/auth/status", nil)
	rec := httptest.NewRecorder()
	setAdminCookie(rec, req, "token-value")
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one auth cookie, got %d", len(cookies))
	}
	if cookies[0].Name != adminCookieName || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("auth cookie flags are not hardened: %#v", cookies[0])
	}
	if decodeCookieToken(cookies[0].Value) != "token-value" {
		t.Fatalf("auth cookie token did not round trip")
	}
}
