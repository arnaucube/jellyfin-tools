package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelreview/internal/source"
)

func credentials(t *testing.T) (string, CredentialFile) {
	t.Helper()
	b, e := os.ReadFile("../../testdata/offline-auth.json")
	if e != nil {
		t.Fatal(e)
	}
	var f CredentialFile
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "users.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	return path, f
}
func replaceCredentials(t *testing.T, path string, f CredentialFile) {
	t.Helper()
	b, e := json.Marshal(f)
	if e != nil {
		t.Fatal(e)
	}
	temp := path + ".new"
	if e = os.WriteFile(temp, b, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(temp, path); e != nil {
		t.Fatal(e)
	}
}
func TestOfflineJellyfinPasswordVectors(t *testing.T) {
	path, _ := credentials(t)
	p, e := NewOffline(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct{ name, password, id string }{{"ALICE", "fixture-only", strings.Repeat("a", 32)}, {"Legacy", "pässwörd 🎬", strings.Repeat("c", 32)}} {
		a, revision, e := p.Login(context.Background(), tt.name, tt.password, "unused")
		if e != nil || a.ID != tt.id || revision == "" {
			t.Fatal("Python-derived Jellyfin password vector did not authenticate", e)
		}
		if _, e = p.Validate(context.Background(), Session{Account: a, Token: revision}); e != nil {
			t.Fatal(e)
		}
	}
	for _, tt := range []struct{ name, password string }{{"Alice", "wrong"}, {"Unknown", "fixture-only"}, {"Disabled", ""}, {"Alice", ""}} {
		_, _, e = p.Login(context.Background(), tt.name, tt.password, "")
		if !errors.Is(e, source.ErrDenied) {
			t.Fatal("unknown/disabled/incorrect login did not fail generically")
		}
	}
}
func TestOfflineReplacementsRevokeOnlyAffectedAccounts(t *testing.T) {
	for _, change := range []string{"password", "rename", "remove", "disable"} {
		t.Run(change, func(t *testing.T) {
			path, file := credentials(t)
			p, e := NewOffline(path)
			if e != nil {
				t.Fatal(e)
			}
			a, token, e := p.Login(context.Background(), "Alice", "fixture-only", "")
			if e != nil {
				t.Fatal(e)
			}
			s := Session{Account: a, Token: token}
			// Metadata refresh and other users changing must leave Alice's revision usable.
			file.ExportedAt = file.ExportedAt.Add(time.Hour)
			file.Users[1].Username = "Other name"
			replaceCredentials(t, path, file)
			if _, e = p.Validate(context.Background(), s); e != nil {
				t.Fatal("unrelated refresh revoked Alice")
			}
			switch change {
			case "password":
				file.Users[0].PasswordHash = strings.Replace(file.Users[0].PasswordHash, "001122", "112233", 1)
			case "rename":
				file.Users[0].Username = "Renamed"
			case "remove":
				file.Users = file.Users[1:]
			case "disable":
				file.Users[0].Disabled = true
				file.Users[0].PasswordHash = ""
			}
			replaceCredentials(t, path, file)
			if _, e = p.Validate(context.Background(), s); !errors.Is(e, source.ErrDenied) {
				t.Fatal("changed account retained session")
			}
		})
	}
}
func TestOfflineFailsClosedWithoutLeakingVerifier(t *testing.T) {
	path, file := credentials(t)
	p, e := NewOffline(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*CredentialFile){
		func(f *CredentialFile) { f.Version = 99 }, func(f *CredentialFile) { f.SourceVersion = "future" },
		func(f *CredentialFile) { f.Users = append(f.Users, f.Users[0]) }, func(f *CredentialFile) { f.Users[0].PasswordHash = "plaintext-password" },
		func(f *CredentialFile) {
			f.Users[0].PasswordHash = strings.Replace(f.Users[0].PasswordHash, "210000", "1000001", 1)
		},
		func(f *CredentialFile) { f.Users[0].ID = "../../escape" },
	} {
		copy := file
		copy.Users = append([]Credential{}, file.Users...)
		mutate(&copy)
		replaceCredentials(t, path, copy)
		if _, _, e = p.Login(context.Background(), "Alice", "fixture-only", ""); !errors.Is(e, source.ErrUnavailable) || strings.Contains(e.Error(), "plaintext") || strings.Contains(e.Error(), "PBKDF2") {
			t.Fatal("malformed credentials did not fail closed/generically")
		}
	}
	replaceCredentials(t, path, file)
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = NewOffline(path); e == nil {
		t.Fatal("publicly readable credentials accepted")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if e = os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e = NewOffline(link); e == nil {
		t.Fatal("credential symlink accepted")
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Validate(context.Background(), Session{}); !errors.Is(e, source.ErrUnavailable) {
		t.Fatal("missing file fell back to credentials")
	}
}
func TestOfflineBoundedVerificationAndUnicodeNames(t *testing.T) {
	path, f := credentials(t)
	f.Users[0].Username = "Vïewer"
	replaceCredentials(t, path, f)
	p, e := NewOffline(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = p.Login(context.Background(), "Vïewer", "fixture-only", ""); e != nil {
		t.Fatal("exact Unicode username failed")
	}
	if loginKey("Vïewer") == loginKey("vïewer") {
		t.Fatal("ambiguous Unicode casing was normalized")
	}
	p.work <- struct{}{}
	p.work <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = p.Login(ctx, "Vïewer", "fixture-only", ""); !errors.Is(e, source.ErrUnavailable) {
		t.Fatal("queued canceled verification did not stop")
	}
}

func TestOfflineRequiresExplicitAccountStatus(t *testing.T) {
	path, _ := credentials(t)
	p, e := NewOffline(path)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var document map[string]any
	if e = json.Unmarshal(b, &document); e != nil {
		t.Fatal(e)
	}
	user := document["users"].([]any)[0].(map[string]any)
	delete(user, "disabled")
	b, e = json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = p.load(); !errors.Is(e, source.ErrUnavailable) {
		t.Fatal("missing account status became enabled")
	}
}
