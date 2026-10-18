package auth

import (
	"bytes"
	"context"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reelreview/internal/model"
	"reelreview/internal/source"
)

const maxAuthBytes = 1 << 20

type Credential struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Disabled     bool   `json:"disabled"`
}

// UnmarshalJSON requires explicit account status rather than treating a missing
// disabled flag as an enabled account in a malformed private export.
func (c *Credential) UnmarshalJSON(b []byte) error {
	var fields struct {
		ID           *string `json:"id"`
		Username     *string `json:"username"`
		PasswordHash *string `json:"passwordHash"`
		Disabled     *bool   `json:"disabled"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&fields) != nil || fields.ID == nil || fields.Username == nil || fields.PasswordHash == nil || fields.Disabled == nil {
		return source.ErrUnavailable
	}
	*c = Credential{ID: *fields.ID, Username: *fields.Username, PasswordHash: *fields.PasswordHash, Disabled: *fields.Disabled}
	return nil
}

type CredentialFile struct {
	Version       int          `json:"schemaVersion"`
	SourceVersion string       `json:"sourceVersion"`
	ExportedAt    time.Time    `json:"exportedAt"`
	Users         []Credential `json:"users"`
}
type verifier struct {
	algorithm  string
	iterations int
	salt, hash []byte
}
type offlineUser struct {
	credential Credential
	verifier   verifier
	revision   string
}
type offlineUsers struct {
	byName map[string]offlineUser
	byID   map[string]offlineUser
}

// Offline rereads a private, atomically replaced file for every authentication
// check. Invalid or missing files fail closed without cached credential fallback.
type Offline struct {
	path string
	work chan struct{}
}

func NewOffline(path string) (*Offline, error) {
	absolute, e := filepath.Abs(path)
	if e != nil || path == "" {
		return nil, fmt.Errorf("offline mode requires a private credential file")
	}
	p := &Offline{path: absolute, work: make(chan struct{}, 2)}
	if _, e = p.load(); e != nil {
		return nil, fmt.Errorf("cannot load offline credentials: check file format, path and private permissions")
	}
	return p, nil
}
func (p *Offline) Mode() string { return "offline" }
func (p *Offline) Path() string { return p.path }

// loginKey preserves exact spelling for non-ASCII names to avoid differing .NET
// and Go Unicode normalization versions. ASCII account names ignore case.
func loginKey(name string) string {
	for _, r := range name {
		if r > 127 {
			return "unicode:" + name
		}
	}
	return "ascii:" + strings.ToLower(name)
}

// parseVerifier accepts Jellyfin hex PBKDF2 strings within bounded work parameters.
func parseVerifier(encoded string) (verifier, error) {
	var v verifier
	fields := strings.Split(encoded, "$")
	if len(fields) != 5 || fields[0] != "" || !strings.HasPrefix(fields[2], "iterations=") {
		return v, source.ErrUnavailable
	}
	v.algorithm = fields[1]
	size := 64
	if v.algorithm == "PBKDF2" {
		size = 32
	} else if v.algorithm != "PBKDF2-SHA512" {
		return v, source.ErrUnavailable
	}
	var e error
	v.iterations, e = strconv.Atoi(strings.TrimPrefix(fields[2], "iterations="))
	if e != nil || v.iterations < 1 || v.iterations > 1000000 {
		return v, source.ErrUnavailable
	}
	v.salt, e = hex.DecodeString(fields[3])
	if e != nil || len(v.salt) < 8 || len(v.salt) > 128 {
		return v, source.ErrUnavailable
	}
	v.hash, e = hex.DecodeString(fields[4])
	if e != nil || len(v.hash) != size {
		return v, source.ErrUnavailable
	}
	return v, nil
}
func revision(c Credential) string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// load validates the complete current file before any account is trusted.
func (p *Offline) load() (offlineUsers, error) {
	users := offlineUsers{byName: map[string]offlineUser{}, byID: map[string]offlineUser{}}
	// Refuse symlinks and group/world-readable files, including operator mistakes.
	st, e := os.Lstat(p.path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > maxAuthBytes {
		return users, source.ErrUnavailable
	}
	f, e := os.Open(p.path)
	if e != nil {
		return users, source.ErrUnavailable
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(st, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return users, source.ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(f, maxAuthBytes+1))
	if e != nil || len(b) > maxAuthBytes {
		return users, source.ErrUnavailable
	}
	var file CredentialFile
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&file) != nil || d.Decode(new(any)) != io.EOF || file.Version != 1 || file.SourceVersion != "12.2" || file.ExportedAt.IsZero() || file.Users == nil || len(file.Users) > 1000 {
		return users, source.ErrUnavailable
	}
	for _, c := range file.Users {
		id, e := model.ID(c.ID)
		if e != nil || id != c.ID || c.Username == "" || len(c.Username) > 256 || !utf8.ValidString(c.Username) || strings.ContainsAny(c.Username, "\x00\r\n") {
			return users, source.ErrUnavailable
		}
		key := loginKey(c.Username)
		if _, ok := users.byName[key]; ok {
			return users, source.ErrUnavailable
		}
		if _, ok := users.byID[id]; ok {
			return users, source.ErrUnavailable
		}
		var v verifier
		if !c.Disabled {
			v, e = parseVerifier(c.PasswordHash)
			if e != nil {
				return users, source.ErrUnavailable
			}
		} else if c.PasswordHash != "" {
			return users, source.ErrUnavailable
		}
		u := offlineUser{credential: c, verifier: v, revision: revision(c)}
		users.byName[key] = u
		users.byID[id] = u
	}
	return users, nil
}

// Login checks the imported verifier without creating an upstream session.
func (p *Offline) Login(ctx context.Context, name, password, device string) (source.Account, string, error) {
	select {
	case p.work <- struct{}{}:
		defer func() { <-p.work }()
	case <-ctx.Done():
		return source.Account{}, "", source.ErrUnavailable
	}
	users, e := p.load()
	if e != nil {
		return source.Account{}, "", e
	}
	u, ok := users.byName[loginKey(name)]
	// Unknown and disabled accounts perform a real default-cost derivation too.
	v := u.verifier
	if !ok || u.credential.Disabled {
		v = verifier{algorithm: "PBKDF2-SHA512", iterations: 210000, salt: make([]byte, 16), hash: make([]byte, 64)}
	}
	var actual []byte
	if v.algorithm == "PBKDF2" {
		actual, e = pbkdf2.Key(sha1.New, password, v.salt, v.iterations, 32)
	} else {
		actual, e = pbkdf2.Key(sha512.New, password, v.salt, v.iterations, 64)
	}
	if e != nil || ctx.Err() != nil {
		return source.Account{}, "", source.ErrUnavailable
	}
	matches := subtle.ConstantTimeCompare(actual, v.hash) == 1
	if !ok || u.credential.Disabled || !matches {
		return source.Account{}, "", source.ErrDenied
	}
	// Recheck after the expensive derivation so a concurrent replacement can revoke it.
	session := Session{Account: source.Account{ID: u.credential.ID, Name: u.credential.Username}, Token: u.revision}
	account, e := p.Validate(ctx, session)
	if e != nil {
		return source.Account{}, "", e
	}
	return account, u.revision, nil
}

// Validate revokes changed or removed accounts on their next private request.
func (p *Offline) Validate(ctx context.Context, s Session) (source.Account, error) {
	if ctx.Err() != nil {
		return source.Account{}, source.ErrUnavailable
	}
	users, e := p.load()
	if e != nil {
		return source.Account{}, e
	}
	u, ok := users.byID[s.Account.ID]
	if !ok || u.credential.Disabled || subtle.ConstantTimeCompare([]byte(s.Token), []byte(u.revision)) != 1 {
		return source.Account{}, source.ErrDenied
	}
	return source.Account{ID: u.credential.ID, Name: u.credential.Username}, nil
}
func (p *Offline) Logout(context.Context, Session) error { return nil }
