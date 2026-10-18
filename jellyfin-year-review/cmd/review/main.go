// review generates immutable datasets or serves the static recap application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata"

	"reelreview/internal/artwork"
	"reelreview/internal/auth"
	"reelreview/internal/generate"
	"reelreview/internal/model"
	"reelreview/internal/server"
	"reelreview/internal/source"
)

func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "review:", err)
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: review generate|serve [flags]")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "generate":
		f := flag.NewFlagSet("generate", flag.ContinueOnError)
		year := f.Int("year", 0, "selected year (required)")
		output := f.String("output", "./data", "private output directory")
		input := f.String("input", "", "canonical snapshot JSON")
		history := f.String("history", "", "raw Jellystat JSONL snapshot (requires Jellyfin operator API key)")
		captured := f.String("captured-at", "", "Jellystat snapshot timestamp, RFC3339")
		coverage := f.String("coverage", "", "explicit source coverage/retention description")
		timezone := f.String("timezone", env("REVIEW_TIMEZONE", "UTC"), "default IANA timezone")
		zones := f.String("user-timezones", "", "JSON file mapping normalized user IDs to IANA timezones")
		minimum := f.Float64("minimum-seconds", 60, "minimum original record watch time")
		completion := f.Float64("completion-percent", 90, "movie completion threshold")
		gap := f.Duration("marathon-gap", 45*time.Minute, "maximum gap in a marathon")
		cache := f.String("cache", "./cache", "generator-only Jellyfin artwork cache")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		c := generate.DefaultConfig(*year)
		c.Timezone = *timezone
		c.MinimumSeconds = *minimum
		c.Completion = *completion
		c.MarathonGap = *gap
		if *zones != "" {
			b, e := os.ReadFile(*zones)
			if e != nil {
				return fmt.Errorf("cannot read timezone mapping")
			}
			if json.Unmarshal(b, &c.UserTimezones) != nil {
				return fmt.Errorf("invalid timezone mapping")
			}
		}
		if e := c.Validate(); e != nil {
			return e
		}
		if (*input == "") == (*history == "") {
			return fmt.Errorf("choose exactly one of --input or --history")
		}
		var s model.Snapshot
		var jf *source.Jellyfin
		key := os.Getenv("JELLYFIN_API_KEY")
		var e error
		if *input != "" {
			s, e = source.Load(*input)
		} else {
			t, err := time.Parse(time.RFC3339, *captured)
			if err != nil {
				return fmt.Errorf("--captured-at must be the snapshot RFC3339 timestamp")
			}
			if *coverage == "" {
				return fmt.Errorf("--coverage is required for Jellystat history")
			}
			if key == "" {
				return fmt.Errorf("JELLYFIN_API_KEY is required for metadata inventory")
			}
			j, err := source.NewJellyfin(os.Getenv("JELLYFIN_URL"))
			if err != nil {
				return err
			}
			jf = j
			users, items, err := j.Metadata(ctx, key)
			if err != nil {
				return fmt.Errorf("cannot obtain Jellyfin metadata inventory: %w", err)
			}
			s, e = source.Jellystat(*history, users, items, t, *coverage)
		}
		if e != nil {
			return e
		}
		known := map[string]bool{}
		for _, u := range s.Users {
			known[u.ID] = true
		}
		orphaned := 0
		for _, p := range s.Playback {
			if !known[p.UserID] {
				orphaned++
			}
		}
		if orphaned > 0 {
			fmt.Fprintf(os.Stderr, "Skipped %d history records for unavailable/disabled users.\n", orphaned)
		}
		rs, e := generate.Compute(s, c)
		if e != nil {
			return e
		}
		if jf == nil && key != "" && os.Getenv("JELLYFIN_URL") != "" {
			jf, e = source.NewJellyfin(os.Getenv("JELLYFIN_URL"))
			if e != nil {
				return e
			}
		}
		assets, warnings := artwork.New(jf, key, *cache).Resolve(ctx, s.Items, rs)
		if ctx.Err() != nil {
			return fmt.Errorf("generation canceled")
		}
		for _, warning := range warnings {
			fmt.Fprintln(os.Stderr, warning)
		}
		for i := range rs {
			rs[i].Warnings = append(rs[i].Warnings, warnings...)
		}
		if e = generate.Publish(*output, rs, assets); e != nil {
			return e
		}
		fmt.Printf("Published %d private reviews for %d.\n", len(rs), *year)
		return nil
	case "serve":
		f := flag.NewFlagSet("serve", flag.ContinueOnError)
		data := f.String("data", "./data", "private generated dataset")
		web := f.String("web", "./web/build", "static frontend build")
		addr := f.String("listen", ":8080", "HTTP listener")
		origin := f.String("origin", os.Getenv("APP_ORIGIN"), "exact browser origin")
		authMode := f.String("auth-mode", env("REVIEW_AUTH_MODE", "jellyfin"), "authentication backend: jellyfin or offline")
		authFile := f.String("auth-file", os.Getenv("REVIEW_AUTH_FILE"), "private offline credential export (outside data and web)")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		var provider auth.Provider
		switch *authMode {
		case "offline":
			p, e := auth.NewOffline(*authFile)
			if e != nil {
				return e
			}
			provider = p
		case "jellyfin":
			if *authFile != "" {
				return fmt.Errorf("--auth-file requires --auth-mode offline")
			}
			j, e := source.NewJellyfin(os.Getenv("JELLYFIN_URL"))
			if e != nil {
				return e
			}
			provider = auth.Online{Jellyfin: j}
		default:
			return fmt.Errorf("--auth-mode must be jellyfin or offline")
		}
		s, e := server.NewWithAuth(*data, *web, *origin, provider)
		if e != nil {
			return e
		}
		defer s.Close()
		httpServer := server.HTTPServer(*addr, s)
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdown)
		}()
		fmt.Println("Review server listening on " + *addr)
		e = httpServer.ListenAndServe()
		if e == http.ErrServerClosed {
			return nil
		}
		return e
	default:
		return fmt.Errorf("unknown mode %s", strconv.Quote(args[0]))
	}
}
