package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelreview/internal/auth"
	"reelreview/internal/generate"
	"reelreview/internal/model"
	"reelreview/internal/source"
)

const aID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const bID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type harness struct {
	s                    *Server
	revoked, unavailable bool
	logout               int
	authCalls            int
}

func setup(t *testing.T) *harness {
	t.Helper()
	h := &harness{}
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.authCalls++
		if h.unavailable {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/Users/AuthenticateByName":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["Pw"] != "valid" {
				w.WriteHeader(401)
				return
			}
			if !strings.Contains(r.Header.Get("Authorization"), `Client="Reel Review"`) {
				t.Error("missing separate app identity")
			}
			json.NewEncoder(w).Encode(map[string]any{"AccessToken": "private-token", "User": map[string]any{"Id": bID}})
		case "/Users/Me":
			if h.revoked || !strings.Contains(r.Header.Get("Authorization"), `Token="private-token"`) {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"Id": aID, "Name": "Alice", "Policy": map[string]bool{"IsAdministrator": true}})
		case "/Sessions/Logout":
			h.logout++
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(jf.Close)
	j, e := source.NewJellyfin(jf.URL)
	if e != nil {
		t.Fatal(e)
	}
	data := t.TempDir()
	for _, year := range []int{2024, 2025} {
		rs := []model.Review{}
		for _, id := range []string{aID, bID} {
			rs = append(rs, model.Review{Version: 1, User: model.User{ID: id, Name: id}, Year: year, Timezone: "UTC", Availability: map[string]model.Availability{}, Seconds: 3600})
		}
		if e := generate.Publish(data, rs, nil); e != nil {
			t.Fatal(e)
		}
	}
	web := t.TempDir()
	os.WriteFile(filepath.Join(web, "index.html"), []byte(`<html><script>console.log('shell')</script></html>`), 0600)
	h.s, e = New(data, web, "https://review.test", j)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.s.Close() })
	return h
}
func req(h *harness, method, path, body string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.s.ServeHTTP(w, r)
	return w
}
func signin(t *testing.T, h *harness) *http.Cookie {
	t.Helper()
	w := req(h, "POST", "/api/login", `{"username":"Alice","password":"valid"}`, nil, h.s.Origin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("insecure cookie")
	}
	if strings.Contains(w.Body.String(), "token") {
		t.Fatal("token leaked")
	}
	return cookies[0]
}
func TestOwnerAuthorizationAndPrivatePaths(t *testing.T) {
	h := setup(t)
	w := req(h, "GET", "/api/review?year=2025", "", nil, "")
	if w.Code != 401 {
		t.Fatal("unauthenticated review")
	}
	cookie := signin(t, h)
	w = req(h, "GET", "/api/review?year=2025", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), aID) || strings.Contains(w.Body.String(), bID) {
		t.Fatal("actual token identity not used", w.Body.String())
	}
	for _, path := range []string{"/manifest.json", "/data/CURRENT", "/" + bID + "/2025.json", "/api/users", "/art/../../CURRENT", "/art/%2e%2e%2fCURRENT", "/_app/../../CURRENT"} {
		w = req(h, "GET", path, "", cookie, "")
		if w.Code != 404 {
			t.Fatalf("private path %s = %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/review?year=2025&userId=" + bID, "/api/review?year=../../2025", "/api/review?year=2025&year=2024"} {
		if w = req(h, "GET", path, "", cookie, ""); w.Code != 400 {
			t.Fatal("unsafe selector", path, w.Code)
		}
	}
	w = req(h, "GET", "/api/session", "", cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), bID) || !strings.Contains(w.Body.String(), "2024") {
		t.Fatal("session manifest leak or missing year")
	}
	w = req(h, "GET", "/review/2025", "", nil, "")
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sha256-") {
		t.Fatal("deep link or CSP bootstrap broken")
	}
}
func TestExpiryTamperingRevocationAndOutage(t *testing.T) {
	h := setup(t)
	cookie := signin(t, h)
	tampered := *cookie
	tampered.Value = "f" + tampered.Value[1:] + "a"
	if req(h, "GET", "/api/session", "", &tampered, "").Code != 401 {
		t.Fatal("tampered session")
	}
	h.unavailable = true
	if req(h, "GET", "/api/review?year=2025", "", cookie, "").Code != 503 {
		t.Fatal("stale data on auth outage")
	}
	h.unavailable = false
	h.revoked = true
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("revoked token")
	}
	h.revoked = false
	cookie = signin(t, h)
	h.s.Sessions.Now = func() time.Time { return time.Now().Add(13 * time.Hour) }
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("expired app session")
	}
}
func TestLoginCSRFRateLimitsAndLogout(t *testing.T) {
	h := setup(t)
	if req(h, "POST", "/api/login", `{"username":"A","password":"valid"}`, nil, "https://evil.test").Code != 403 {
		t.Fatal("CSRF")
	}
	for i := 0; i < 10; i++ {
		w := req(h, "POST", "/api/login", `{"username":"A","password":"bad"}`, nil, h.s.Origin)
		if w.Code != 401 || strings.Contains(w.Body.String(), "Alice") {
			t.Fatal("non-generic failure")
		}
	}
	if req(h, "POST", "/api/login", `{"username":"A","password":"valid"}`, nil, h.s.Origin).Code != 429 {
		t.Fatal("no login limit")
	}
	h = setup(t)
	cookie := signin(t, h)
	if req(h, "POST", "/api/logout", "{}", cookie, "https://evil.test").Code != 403 {
		t.Fatal("logout CSRF")
	}
	if req(h, "POST", "/api/logout", "{}", cookie, h.s.Origin).Code != 200 || h.logout != 1 {
		t.Fatal("logout did not revoke only app token")
	}
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("local session survived logout")
	}
	cookie = signin(t, h)
	h.unavailable = true
	w := req(h, "POST", "/api/logout", "{}", cookie, h.s.Origin)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "Signed out locally") {
		t.Fatal("outage logout behavior")
	}
	h.unavailable = false
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("outage retained session")
	}
}
func TestRequestBoundsAndUnsupportedDocuments(t *testing.T) {
	h := setup(t)
	if req(h, "POST", "/api/login", `{"username":"`+strings.Repeat("a", 5000)+`","password":"valid"}`, nil, h.s.Origin).Code != 400 {
		t.Fatal("body limit")
	}
	cookie := signin(t, h)
	_, dir, e := generate.Manifest(h.s.Data)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, aID, "2025.json")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	io.WriteString(f, `{"schemaVersion":99}`)
	f.Close()
	if req(h, "GET", "/api/review?year=2025", "", cookie, "").Code != 503 {
		t.Fatal("unsupported schema accepted")
	}
}

func offlineHarness(t *testing.T) (*harness, string, auth.CredentialFile) {
	t.Helper()
	h := setup(t)
	b, e := os.ReadFile("../../testdata/offline-auth.json")
	if e != nil {
		t.Fatal(e)
	}
	var file auth.CredentialFile
	if e = json.Unmarshal(b, &file); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "users.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	p, e := auth.NewOffline(path)
	if e != nil {
		t.Fatal(e)
	}
	// Constructor validation must work without constructing any Jellyfin client.
	s, e := NewWithAuth(h.s.Data, h.s.Web, h.s.Origin, p)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	h.s.Auth = p
	h.unavailable = true
	return h, path, file
}
func offlineSignin(t *testing.T, h *harness, name, password string) *http.Cookie {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"username": name, "password": password})
	w := req(h, "POST", "/api/login", string(b), nil, h.s.Origin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func replaceAuth(t *testing.T, path string, file auth.CredentialFile) {
	t.Helper()
	b, _ := json.Marshal(file)
	if e := os.WriteFile(path+".new", b, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(path+".new", path); e != nil {
		t.Fatal(e)
	}
}
func TestOfflineLoginReviewAndLogoutWithNoJellyfin(t *testing.T) {
	h, path, _ := offlineHarness(t)
	w := req(h, "GET", "/api/config", "", nil, "")
	if w.Code != 200 || w.Body.String() != "{\"authMode\":\"offline\"}\n" {
		t.Fatal("config leaks more than auth mode")
	}
	cookie := offlineSignin(t, h, "ALICE", "fixture-only")
	w = req(h, "GET", "/api/review?year=2025", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), aID) || strings.Contains(w.Body.String(), bID) || strings.Contains(w.Body.String(), "PBKDF2") {
		t.Fatal("offline owner boundary or verifier exposure")
	}
	if req(h, "GET", "/api/review?year=2025&userId="+bID, "", cookie, "").Code != 400 {
		t.Fatal("offline impersonation")
	}
	for _, route := range []string{"/auth/users.json", "/users.json", "/auth/users.json.previous", "/api/auth-export"} {
		if req(h, "GET", route, "", cookie, "").Code != 404 {
			t.Fatal("private credentials exposed")
		}
	}
	if req(h, "POST", "/api/logout", "{}", cookie, h.s.Origin).Code != 200 {
		t.Fatal("offline logout needs network")
	}
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("offline session survived logout")
	}
	cookie = offlineSignin(t, h, "Alice", "fixture-only")
	h.s.Sessions.Now = func() time.Time { return time.Now().Add(13 * time.Hour) }
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("offline session expiry")
	}
	if h.authCalls != 0 {
		t.Fatal("offline serving contacted Jellyfin")
	}
	// Never accept credential files in a directory that serving can publish.
	for _, dir := range []string{h.s.Web, h.s.Data} {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		private := filepath.Join(dir, "users.json")
		if e = os.WriteFile(private, b, 0600); e != nil {
			t.Fatal(e)
		}
		p, e := auth.NewOffline(private)
		if e != nil {
			t.Fatal(e)
		}
		if s, e := NewWithAuth(h.s.Data, h.s.Web, h.s.Origin, p); e == nil {
			s.Close()
			t.Fatal("credentials accepted in web/data directory")
		}
	}
}
func TestOfflineCredentialRefreshAndCorruption(t *testing.T) {
	h, path, file := offlineHarness(t)
	cookie := offlineSignin(t, h, "Alice", "fixture-only")
	if e := os.WriteFile(path, []byte(`{"schemaVersion":99}`), 0600); e != nil {
		t.Fatal(e)
	}
	w := req(h, "GET", "/api/review?year=2025", "", cookie, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), aID) {
		t.Fatal("invalid export fell back to old credentials")
	}
	replaceAuth(t, path, file)
	if req(h, "GET", "/api/session", "", cookie, "").Code != 200 {
		t.Fatal("valid restored export unavailable")
	}
	file.Users[0].PasswordHash = file.Users[1].PasswordHash
	replaceAuth(t, path, file)
	if req(h, "GET", "/api/session", "", cookie, "").Code != 401 {
		t.Fatal("password update retained offline session")
	}
	w = req(h, "POST", "/api/login", `{"username":"Alice","password":"fixture-only"}`, nil, h.s.Origin)
	if w.Code != 401 {
		t.Fatal("old offline password survived refresh")
	}
	cookie = offlineSignin(t, h, "Alice", "pässwörd 🎬")
	file.Users[0].Disabled = true
	file.Users[0].PasswordHash = ""
	replaceAuth(t, path, file)
	if req(h, "GET", "/api/review?year=2025", "", cookie, "").Code != 401 {
		t.Fatal("disabled account retained access")
	}
	if h.authCalls != 0 {
		t.Fatal("offline refresh contacted Jellyfin")
	}
}
func TestOfflineLoginCSRFAndRateLimit(t *testing.T) {
	h, _, _ := offlineHarness(t)
	if req(h, "POST", "/api/login", `{"username":"Alice","password":"fixture-only"}`, nil, "https://evil.test").Code != 403 {
		t.Fatal("offline CSRF")
	}
	for i := 0; i < 10; i++ {
		w := req(h, "POST", "/api/login", `{"username":"Alice","password":"wrong"}`, nil, h.s.Origin)
		if w.Code != 401 || strings.Contains(w.Body.String(), "PBKDF2") {
			t.Fatal("offline generic failure")
		}
	}
	if req(h, "POST", "/api/login", `{"username":"Alice","password":"fixture-only"}`, nil, h.s.Origin).Code != 429 {
		t.Fatal("offline rate limit")
	}
}
