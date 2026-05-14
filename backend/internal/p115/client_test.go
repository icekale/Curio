package p115

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"curio/internal/models"
)

func TestFileInfoFromOpenFolderMap(t *testing.T) {
	row := decodeMap(t, `{"fid":"3428179986607432503","fc":0,"fn":"collections","pc":"fednuli5o05t483h77"}`)
	info := fileInfoFromMap(row)
	if info.ID != "3428179986607432503" {
		t.Fatalf("unexpected id %q", info.ID)
	}
	if info.Name != "collections" {
		t.Fatalf("unexpected name %q", info.Name)
	}
	if !info.IsDirectory {
		t.Fatal("expected folder to be detected as directory")
	}
}

func TestFileInfoFromOpenFileMap(t *testing.T) {
	row := decodeMap(t, `{"fid":"3427411743768765275","sha1":"ABC","fn":"movie.iso","fs":62467801088,"pc":"d1yv62zvcl0omxipp","fc":1}`)
	info := fileInfoFromMap(row)
	if info.ID != "3427411743768765275" || info.Name != "movie.iso" {
		t.Fatalf("unexpected file info %#v", info)
	}
	if info.SHA1 != "ABC" || info.Size != 62467801088 || info.PickCode != "d1yv62zvcl0omxipp" {
		t.Fatalf("unexpected media fields %#v", info)
	}
	if info.IsDirectory {
		t.Fatal("expected media file, got directory")
	}
}

func TestCookiesFromLoginResponseMap(t *testing.T) {
	payload := decodeMap(t, `{"state":1,"data":{"cookie":{"UID":"u","CID":"c","SEID":"s","KID":"k"}}}`)
	cookies := cookiesFromLoginResponse(payload, nil)
	for _, want := range []string{"UID=u", "CID=c", "SEID=s", "KID=k"} {
		if !strings.Contains(cookies, want) {
			t.Fatalf("expected %q in %q", want, cookies)
		}
	}
}

func TestCookiesFromLoginResponseHeader(t *testing.T) {
	header := http.Header{}
	header.Add("Set-Cookie", "UID=u; Path=/; HttpOnly")
	header.Add("Set-Cookie", "SEID=s; Path=/; HttpOnly")
	cookies := cookiesFromLoginResponse(nil, header)
	if cookies != "UID=u; SEID=s" {
		t.Fatalf("unexpected cookies %q", cookies)
	}
}

func TestClientPrefersConfiguredAuthMode(t *testing.T) {
	cookieClient := NewClient(models.P115Settings{
		AuthMode:    authModeCookies,
		Cookies:     "UID=u; CID=c; SEID=s",
		AccessToken: "open-token",
	})
	if cookieClient.preferOpen() {
		t.Fatal("expected cookies mode to avoid Open API when cookies are available")
	}

	openClient := NewClient(models.P115Settings{
		AuthMode:    authModeOpen,
		Cookies:     "UID=u; CID=c; SEID=s",
		AccessToken: "open-token",
	})
	if !openClient.preferOpen() {
		t.Fatal("expected open mode to prefer Open API")
	}
}

func decodeMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var row map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil {
		t.Fatal(err)
	}
	return row
}
