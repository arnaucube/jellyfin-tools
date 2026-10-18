// Package model defines the private source snapshot and public-to-owner review v1 contracts.
package model

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const Version = 1

// ID converts Jellyfin UUIDs to lowercase, hyphen-free identity keys.
func ID(s string) (string, error) {
	s = strings.ToLower(strings.ReplaceAll(s, "-", ""))
	if len(s) != 32 {
		return "", fmt.Errorf("invalid Jellyfin UUID")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("invalid Jellyfin UUID")
	}
	return s, nil
}

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Item struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Type        string            `json:"type"`
	SeriesID    string            `json:"seriesId,omitempty"`
	SeriesTitle string            `json:"seriesTitle,omitempty"`
	Year        int               `json:"year,omitempty"`
	Runtime     float64           `json:"runtimeSeconds,omitempty"`
	Genres      []string          `json:"genres"`
	Studios     []string          `json:"studios,omitempty"`
	Platforms   []string          `json:"platforms,omitempty"`
	ImageTag    string            `json:"imageTag,omitempty"`
	ProviderIDs map[string]string `json:"providerIds,omitempty"`
	Poster      string            `json:"poster,omitempty"`
}

// Playback is a finalized activity row, not a progress update. Exact records describe
// uninterrupted watched intervals; position ranges describe actual media coverage.
type Playback struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	ItemID        string    `json:"itemId"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	ObservedAt    time.Time `json:"observedAt"`
	Seconds       float64   `json:"seconds"`
	Exact         bool      `json:"exactInterval"`
	PositionStart *float64  `json:"positionStart,omitempty"`
	PositionEnd   *float64  `json:"positionEnd,omitempty"`
	Device        string    `json:"device"`
	Client        string    `json:"client"`
}
type Snapshot struct {
	Version    int        `json:"schemaVersion"`
	CapturedAt time.Time  `json:"capturedAt"`
	Source     string     `json:"source"`
	Coverage   string     `json:"coverage"`
	Users      []User     `json:"users"`
	Items      []Item     `json:"items"`
	Playback   []Playback `json:"playback"`
}
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type Named struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Records int     `json:"records"`
}

// Platform counts distinct watched movies and series associated with a service.
type Platform struct {
	Name   string `json:"name"`
	Movies int    `json:"movies"`
	Shows  int    `json:"shows"`
}
type Content struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Poster       string   `json:"poster,omitempty"`
	PosterSource string   `json:"posterSource,omitempty"`
	Seconds      float64  `json:"seconds"`
	Records      int      `json:"records"`
	Months       []int    `json:"months,omitempty"`
	Completion   *float64 `json:"completionPercent,omitempty"`
}
type Marathon struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Watched float64   `json:"watchedSeconds"`
	Elapsed float64   `json:"elapsedSeconds"`
	Records int       `json:"records"`
}
type Personality struct {
	Name        string `json:"name"`
	Explanation string `json:"explanation"`
}
type Ranking struct {
	Rank       int     `json:"rank"`
	Cohort     int     `json:"cohort"`
	Percentile float64 `json:"percentile"`
	Average    float64 `json:"averageSeconds"`
}
type Rules struct {
	MinimumSeconds     float64 `json:"minimumSeconds"`
	CompletionPercent  float64 `json:"completionPercent"`
	MarathonGapSeconds float64 `json:"marathonGapSeconds"`
}
type Review struct {
	Rules              Rules                   `json:"rules"`
	Version            int                     `json:"schemaVersion"`
	User               User                    `json:"user"`
	Year               int                     `json:"year"`
	Timezone           string                  `json:"timezone"`
	GeneratedAt        time.Time               `json:"generatedAt"`
	Source             string                  `json:"source"`
	Coverage           string                  `json:"coverage"`
	Availability       map[string]Availability `json:"availability"`
	Seconds            float64                 `json:"seconds"`
	MovieSeconds       float64                 `json:"movieSeconds"`
	TVSeconds          float64                 `json:"tvSeconds"`
	Records            int                     `json:"records"`
	UniqueTitles       int                     `json:"uniqueTitles"`
	ActiveDays         int                     `json:"activeDays"`
	Movies             []Content               `json:"movies"`
	Shows              []Content               `json:"shows"`
	Platforms          []Platform              `json:"platforms,omitempty"`
	Abandoned          []Content               `json:"abandoned"`
	Genres             []Named                 `json:"genres"`
	Devices            []Named                 `json:"devices"`
	Clients            []Named                 `json:"clients"`
	Hourly             [24]float64             `json:"hourly"`
	Weekdays           [7]float64              `json:"weekdays"`
	Monthly            [12]float64             `json:"monthly"`
	DistributionMetric string                  `json:"distributionMetric"`
	Marathon           *Marathon               `json:"marathon"`
	Personality        *Personality            `json:"personality"`
	Ranking            *Ranking                `json:"ranking"`
	Warnings           []string                `json:"warnings"`
}
type Manifest struct {
	Version int              `json:"schemaVersion"`
	Users   map[string][]int `json:"users"`
}

// Validate rejects incompatible, corrupted or incorrectly addressed documents.
func (r Review) Validate(id string, year int) error {
	if r.Version != Version || r.User.ID != id || r.Year != year || r.Seconds < 0 || r.Availability == nil {
		return fmt.Errorf("invalid or unsupported review")
	}
	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return fmt.Errorf("invalid review timezone")
	}
	return nil
}
