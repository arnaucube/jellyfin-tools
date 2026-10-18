package source

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"reelreview/internal/model"
)

func ItemKey(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", "")) }

// Load validates a finalized canonical snapshot; inconsistent duplicates fail closed.
func Load(path string) (model.Snapshot, error) {
	var s model.Snapshot
	f, err := os.Open(path)
	if err != nil {
		return s, fmt.Errorf("cannot open source snapshot")
	}
	defer f.Close()
	if st, e := f.Stat(); e != nil || st.Size() > 256<<20 {
		return s, fmt.Errorf("source snapshot exceeds size limit or cannot be read")
	}
	d := json.NewDecoder(io.LimitReader(f, 256<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, fmt.Errorf("invalid source snapshot: %w", err)
	}
	if d.Decode(new(any)) != io.EOF {
		return s, fmt.Errorf("trailing or oversized source data")
	}
	return s, Validate(&s)
}
func Validate(s *model.Snapshot) error {
	if s.Version != 1 || s.CapturedAt.IsZero() || s.Source == "" || s.Coverage == "" {
		return fmt.Errorf("snapshot requires v1, capturedAt, source and explicit coverage")
	}
	users := map[string]bool{}
	for i := range s.Users {
		u := &s.Users[i]
		id, err := model.ID(u.ID)
		if err != nil || users[id] {
			return fmt.Errorf("invalid or duplicate source user")
		}
		u.ID = id
		users[id] = true
	}
	items := map[string]bool{}
	for i := range s.Items {
		v := &s.Items[i]
		v.ID = ItemKey(v.ID)
		v.SeriesID = ItemKey(v.SeriesID)
		if v.ID == "" || items[v.ID] || v.Runtime < 0 || math.IsNaN(v.Runtime) || math.IsInf(v.Runtime, 0) {
			return fmt.Errorf("invalid or duplicate media")
		}
		items[v.ID] = true
	}
	seen := map[string]string{}
	out := []model.Playback{}
	for _, p := range s.Playback {
		var err error
		p.UserID, err = model.ID(p.UserID)
		if err != nil {
			return fmt.Errorf("invalid playback user")
		}
		p.ItemID = ItemKey(p.ItemID)
		if p.ID == "" || p.ItemID == "" || math.IsNaN(p.Seconds) || math.IsInf(p.Seconds, 0) || p.Seconds <= 0 || p.Seconds > 366*86400 {
			return fmt.Errorf("invalid playback duration or identity")
		}
		if p.Exact {
			if !p.End.After(p.Start) || p.Start.IsZero() || math.Abs(p.End.Sub(p.Start).Seconds()-p.Seconds) > 0.01 {
				return fmt.Errorf("exact intervals must match watched seconds")
			}
		} else if p.ObservedAt.IsZero() {
			return fmt.Errorf("aggregate activity requires observedAt")
		}
		if (p.PositionStart == nil) != (p.PositionEnd == nil) {
			return fmt.Errorf("both position bounds required")
		}
		if p.PositionStart != nil && (math.IsNaN(*p.PositionStart) || math.IsInf(*p.PositionStart, 0) || *p.PositionStart < 0 || *p.PositionEnd < *p.PositionStart || math.IsNaN(*p.PositionEnd) || math.IsInf(*p.PositionEnd, 0)) {
			return fmt.Errorf("invalid position range")
		}
		b, _ := json.Marshal(p)
		key := p.UserID + ":" + p.ID
		if prev, ok := seen[key]; ok {
			if prev != string(b) {
				return fmt.Errorf("conflicting duplicate playback ID")
			}
			continue
		}
		seen[key] = string(b)
		out = append(out, p)
	}
	s.Playback = out
	sort.Slice(s.Users, func(i, j int) bool { return s.Users[i].ID < s.Users[j].ID })
	sort.Slice(s.Items, func(i, j int) bool { return s.Items[i].ID < s.Items[j].ID })
	sort.Slice(s.Playback, func(i, j int) bool {
		a, b := s.Playback[i], s.Playback[j]
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		return a.ID < b.ID
	})
	return nil
}

// Jellystat reads the raw table JSONL contract, deliberately rejecting grouped API
// history: grouped durations and resumed records cannot recover exact watch intervals.
func Jellystat(path string, users []model.User, items []model.Item, captured time.Time, coverage string) (model.Snapshot, error) {
	s := model.Snapshot{Version: 1, CapturedAt: captured, Source: "Jellystat raw finalized activity", Coverage: coverage, Users: users, Items: items}
	f, err := os.Open(path)
	if err != nil {
		return s, fmt.Errorf("cannot open Jellystat export")
	}
	defer f.Close()
	if st, e := f.Stat(); e != nil || st.Size() > 256<<20 {
		return s, fmt.Errorf("Jellystat snapshot exceeds size limit or cannot be read")
	}
	scan := bufio.NewScanner(io.LimitReader(f, 256<<20))
	scan.Buffer(make([]byte, 4096), 1<<20)
	for scan.Scan() {
		var row struct {
			ID       string      `json:"Id"`
			UserID   string      `json:"UserId"`
			Item     string      `json:"NowPlayingItemId"`
			Episode  string      `json:"EpisodeId"`
			Duration json.Number `json:"PlaybackDuration"`
			At       time.Time   `json:"ActivityDateInserted"`
			Device   string      `json:"DeviceName"`
			Client   string      `json:"Client"`
			Imported bool        `json:"Imported"`
		}
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			return s, fmt.Errorf("invalid Jellystat raw row")
		}
		if row.Imported {
			return s, fmt.Errorf("Playback Reporting imports are not supported; export native Jellystat activity only")
		}
		n, err := strconv.ParseFloat(string(row.Duration), 64)
		if err != nil {
			return s, fmt.Errorf("invalid Jellystat PlaybackDuration")
		}
		if row.Episode != "" {
			row.Item = row.Episode
		}
		s.Playback = append(s.Playback, model.Playback{ID: row.ID, UserID: row.UserID, ItemID: row.Item, ObservedAt: row.At, Seconds: n, Device: row.Device, Client: row.Client})
	}
	if scan.Err() != nil {
		return s, fmt.Errorf("Jellystat export exceeds row limit or cannot be read")
	}
	return s, Validate(&s)
}
