package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"reelreview/internal/model"
)

var datasetName = regexp.MustCompile(`^[a-f0-9]{64}$`)
var assetName = regexp.MustCompile(`^[a-f0-9]{64}\.jpg$`)

func SafeAsset(s string) bool { return assetName.MatchString(s) }

// Current resolves an atomic pointer to a complete immutable dataset.
func Current(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "CURRENT"))
	if err != nil {
		return "", fmt.Errorf("no published dataset")
	}
	name := string(b)
	if !datasetName.MatchString(name) {
		return "", fmt.Errorf("invalid dataset pointer")
	}
	return filepath.Join(root, "datasets", name), nil
}
func Manifest(root string) (model.Manifest, string, error) {
	var m model.Manifest
	p, err := Current(root)
	if err != nil {
		return m, "", err
	}
	b, err := os.ReadFile(filepath.Join(p, "manifest.json"))
	if err != nil {
		return m, "", err
	}
	if json.Unmarshal(b, &m) != nil || m.Version != 1 || m.Users == nil {
		return m, "", fmt.Errorf("unsupported manifest")
	}
	for id, years := range m.Users {
		n, e := model.ID(id)
		if e != nil || n != id {
			return m, "", fmt.Errorf("invalid manifest identity")
		}
		for _, y := range years {
			if y < 1970 || y > 9998 {
				return m, "", fmt.Errorf("invalid manifest year")
			}
		}
	}
	return m, p, nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return durableWrite(path, b, 0600)
}
func durableWrite(path string, b []byte, mode os.FileMode) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// Publish clones the last successful dataset, replaces one year, then switches a
// synced pointer. A lock serializes writers; a failed write never switches readers.
func Publish(root string, rs []model.Review, assets map[string][]byte) error {
	if len(rs) == 0 {
		return fmt.Errorf("no users to publish")
	}
	if err := os.MkdirAll(filepath.Join(root, "datasets"), 0700); err != nil {
		return err
	}
	lock, e := os.OpenFile(filepath.Join(root, ".generate.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("another generator is active (or stale .generate.lock exists)")
	}
	lock.Close()
	defer os.Remove(filepath.Join(root, ".generate.lock"))
	stage, e := os.MkdirTemp(filepath.Join(root, "datasets"), ".stage-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	m := model.Manifest{Version: 1, Users: map[string][]int{}}
	if _, err := os.Stat(filepath.Join(root, "CURRENT")); err == nil {
		previous, p, err := Manifest(root)
		if err != nil {
			return err
		}
		m = previous
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(p, path)
			if rel == "." {
				return nil
			}
			dest := filepath.Join(stage, rel)
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlinks forbidden in dataset")
			}
			if d.IsDir() {
				return os.MkdirAll(dest, 0700)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return durableWrite(dest, b, 0600)
		})
		if err != nil {
			return err
		}
	}
	year := rs[0].Year
	// Remove that year's previous cohort, including users removed from Jellyfin.
	for id, years := range m.Users {
		keep := []int{}
		for _, y := range years {
			if y != year {
				keep = append(keep, y)
			}
		}
		m.Users[id] = keep
		_ = os.Remove(filepath.Join(stage, id, strconv.Itoa(year)+".json"))
		if len(keep) == 0 {
			delete(m.Users, id)
		}
	}
	for _, r := range rs {
		id, e := model.ID(r.User.ID)
		if e != nil || id != r.User.ID || r.Year != year {
			return fmt.Errorf("inconsistent review cohort")
		}
		if e = r.Validate(id, year); e != nil {
			return e
		}
		dir := filepath.Join(stage, id)
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		if e = writeJSON(filepath.Join(dir, strconv.Itoa(year)+".json"), r); e != nil {
			return e
		}
		m.Users[id] = append(m.Users[id], year)
		sort.Ints(m.Users[id])
	}
	if e = os.MkdirAll(filepath.Join(stage, "art"), 0700); e != nil {
		return e
	}
	for name, b := range assets {
		if !SafeAsset(name) {
			return fmt.Errorf("unsafe artwork name")
		}
		if e = durableWrite(filepath.Join(stage, "art", name), b, 0600); e != nil {
			return e
		}
	}
	if e = writeJSON(filepath.Join(stage, "manifest.json"), m); e != nil {
		return e
	}
	h := sha256.New()
	e = filepath.WalkDir(stage, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return syncDir(path)
		}
		rel, _ := filepath.Rel(stage, path)
		_, _ = io.WriteString(h, rel+"\x00")
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = h.Write(b)
		return nil
	})
	if e != nil {
		return e
	}
	name := hex.EncodeToString(h.Sum(nil))
	dest := filepath.Join(root, "datasets", name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if e = os.Rename(stage, dest); e != nil {
			return e
		}
	}
	if e = syncDir(filepath.Join(root, "datasets")); e != nil {
		return e
	}
	temp, e := os.CreateTemp(root, ".current-")
	if e != nil {
		return e
	}
	path := temp.Name()
	temp.Close()
	defer os.Remove(path)
	if e = durableWrite(path, []byte(name), 0600); e != nil {
		return e
	}
	if e = os.Rename(path, filepath.Join(root, "CURRENT")); e != nil {
		return e
	}
	return syncDir(root)
}
