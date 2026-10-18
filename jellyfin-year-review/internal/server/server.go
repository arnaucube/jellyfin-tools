// Package server serves only static frontend files and authenticated owner-scoped APIs.
package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"reelreview/internal/auth"
	"reelreview/internal/generate"
	"reelreview/internal/model"
	"reelreview/internal/source"
)

type Server struct {
	Data, Web, Origin string
	Auth              auth.Provider
	Sessions          *auth.Store
	Limiter           *auth.Limiter
	secure            bool
	shell             []byte
	csp               string
	webRoot           *os.Root
}

func New(data, web, origin string, j *source.Jellyfin) (*Server, error) {
	if j == nil {
		return nil, fmt.Errorf("Jellyfin authentication is not configured")
	}
	return NewWithAuth(data, web, origin, auth.Online{Jellyfin: j})
}

// NewWithAuth requires an explicit backend; offline mode has no Jellyfin client.
func NewWithAuth(data, web, origin string, provider auth.Provider) (*Server, error) {
	if provider == nil {
		return nil, fmt.Errorf("authentication is not configured")
	}
	if offline, ok := provider.(*auth.Offline); ok {
		for _, dir := range []string{web, data} {
			root, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			private, err := filepath.EvalSymlinks(offline.Path())
			if err != nil {
				return nil, fmt.Errorf("offline credential file unavailable")
			}
			rel, err := filepath.Rel(root, private)
			if err != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..") {
				return nil, fmt.Errorf("offline credentials must be outside the frontend and review data directories")
			}
		}
	}
	u, e := url.Parse(origin)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, fmt.Errorf("APP_ORIGIN must be an exact HTTP(S) origin without a trailing slash")
	}
	root, e := os.OpenRoot(web)
	if e != nil {
		return nil, fmt.Errorf("frontend build missing")
	}
	f, e := root.Open("index.html")
	if e != nil {
		root.Close()
		return nil, fmt.Errorf("frontend index.html missing")
	}
	shell, e := io.ReadAll(io.LimitReader(f, 1<<20))
	f.Close()
	if e != nil {
		root.Close()
		return nil, e
	}
	csp := "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'"
	re := regexp.MustCompile(`(?s)<script\b[^>]*>(.*?)</script>`)
	for _, m := range re.FindAllSubmatch(shell, -1) {
		if len(m[1]) > 0 {
			h := sha256.Sum256(m[1])
			csp += " 'sha256-" + base64.StdEncoding.EncodeToString(h[:]) + "'"
		}
	}
	return &Server{Data: data, Web: web, Origin: origin, Auth: provider, Sessions: auth.New(), Limiter: auth.NewLimiter(), secure: u.Scheme == "https", shell: shell, csp: csp, webRoot: root}, nil
}
func (s *Server) authUnavailable(w http.ResponseWriter) {
	if s.Auth.Mode() == "offline" {
		fail(w, 503, "Offline authentication is unavailable. Ask the operator to check the private credential export.")
		return
	}
	fail(w, 503, "Jellyfin authentication is unavailable. Please retry.")
}
func (s *Server) Close() error { return s.webRoot.Close() }
func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) cookie(w http.ResponseWriter, id string, remove bool) {
	max := int(s.Sessions.TTL.Seconds())
	if remove {
		max = -1
	}
	http.SetCookie(w, &http.Cookie{Name: "review_session", Value: id, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode, MaxAge: max})
}
func cookieID(r *http.Request) string {
	c, e := r.Cookie("review_session")
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	id := cookieID(r)
	v, ok := s.Sessions.Get(id)
	if !ok {
		fail(w, 401, "Your session has expired. Please sign in.")
		return v, false
	}
	a, err := s.Auth.Validate(r.Context(), v)
	if err != nil || a.ID != v.Account.ID {
		if errors.Is(err, source.ErrUnavailable) {
			s.authUnavailable(w)
		} else {
			s.Sessions.Delete(id)
			s.cookie(w, "", true)
			fail(w, 401, "Your session has expired. Please sign in.")
		}
		return v, false
	}
	v.Account = a
	return v, true
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", s.csp)
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if s.secure {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "POST" {
		fail(w, 405, "Method not allowed")
		return
	}
	if r.Method == "POST" {
		if r.Header.Get("Origin") != s.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, "Invalid request origin")
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			fail(w, 415, "Expected application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
	}
	path := r.URL.Path
	if path == "/api/config" && r.Method == "GET" {
		respond(w, map[string]string{"authMode": s.Auth.Mode()})
		return
	}
	if path == "/api/login" && r.Method == "POST" {
		s.login(w, r)
		return
	}
	if path == "/api/logout" && r.Method == "POST" {
		id := cookieID(r)
		v, ok := s.Sessions.Get(id)
		s.Sessions.Delete(id)
		s.cookie(w, "", true)
		if ok {
			if e := s.Auth.Logout(r.Context(), v); e != nil && !errors.Is(e, source.ErrDenied) {
				fail(w, 503, "Signed out locally. Jellyfin token revocation failed; revoke the Reel Review device in Jellyfin.")
				return
			}
		}
		respond(w, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(path, "/api/") {
		if r.Method != "GET" {
			fail(w, 405, "Method not allowed")
			return
		}
		if path != "/api/session" && path != "/api/review" {
			fail(w, 404, "Not found")
			return
		}
		v, ok := s.session(w, r)
		if !ok {
			return
		}
		m, dir, e := generate.Manifest(s.Data)
		if e != nil {
			fail(w, 503, "No valid review dataset is available. Ask the operator to generate one.")
			return
		}
		years := m.Users[v.Account.ID]
		if years == nil {
			years = []int{}
		}
		if path == "/api/session" {
			respond(w, map[string]any{"user": model.User{ID: v.Account.ID, Name: v.Account.Name}, "years": years})
			return
		}
		q := r.URL.Query()
		year, e := strconv.Atoi(q.Get("year"))
		if e != nil || year < 1970 || year > 9998 || len(q) != 1 || len(q["year"]) != 1 {
			fail(w, 400, "Invalid year")
			return
		}
		found := false
		for _, y := range years {
			if y == year {
				found = true
			}
		}
		if !found {
			fail(w, 404, "No review was generated for this year.")
			return
		}
		b, e := os.ReadFile(filepath.Join(dir, v.Account.ID, strconv.Itoa(year)+".json"))
		if e != nil {
			fail(w, 503, "Review unavailable")
			return
		}
		var review model.Review
		if json.Unmarshal(b, &review) != nil || review.Validate(v.Account.ID, year) != nil {
			fail(w, 503, "Unsupported or invalid review format")
			return
		}
		respond(w, review)
		return
	}
	if strings.HasPrefix(path, "/art/") && r.Method == "GET" {
		name := strings.TrimPrefix(path, "/art/")
		if !generate.SafeAsset(name) {
			http.NotFound(w, r)
			return
		}
		dir, e := generate.Current(s.Data)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		b, e := os.ReadFile(filepath.Join(dir, "art", name))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(b)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") || strings.HasPrefix(path, "/.") || strings.HasPrefix(path, "/data") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path, "/")
	if strings.HasPrefix(path, "/_app/") || path == "/favicon.svg" {
		f, e := s.webRoot.Open(name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, name, st.ModTime(), f)
		return
	}
	if path != "/" && path != "/review" && !regexp.MustCompile(`^/review/[0-9]{4}/?$`).MatchString(path) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method != "HEAD" {
		_, _ = w.Write(s.shell)
	}
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.Limiter.Allow(ip) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "Too many sign-in attempts. Try again in a minute.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil || d.Decode(new(any)) != io.EOF || body.Username == "" || len(body.Username) > 256 {
		fail(w, 400, "Invalid login request")
		return
	}
	device := auth.Random()
	a, token, e := s.Auth.Login(r.Context(), body.Username, body.Password, device)
	body.Password = ""
	if e != nil {
		if errors.Is(e, source.ErrUnavailable) {
			s.authUnavailable(w)
		} else {
			fail(w, 401, "Unable to sign in with those credentials.")
		}
		return
	}
	id, ok := s.Sessions.Create(a, token, device)
	if !ok {
		_ = s.Auth.Logout(r.Context(), auth.Session{Account: a, Token: token, Device: device})
		fail(w, 503, "Session capacity reached. Try again later.")
		return
	}
	if old := cookieID(r); old != "" {
		if previous, exists := s.Sessions.Get(old); exists {
			_ = s.Auth.Logout(r.Context(), previous)
		}
		s.Sessions.Delete(old)
	}
	s.cookie(w, id, false)
	respond(w, map[string]bool{"ok": true})
}

// HTTPServer applies bounds even when the app is behind a reverse proxy.
func HTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
