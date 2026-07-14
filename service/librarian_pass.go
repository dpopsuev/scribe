package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	defaultLibrarianMaxAge = 30 * 24 * time.Hour
	librarianPassLimit     = 50
	librarianListLimit     = 500
	logKeyResult           = "result"
	logKeyValue            = "value"
	statusArchived         = "archived"
	statusArchivedPrefixed = "status:archived"
)

// LibrarianPassOpts controls a compaction pass over idle edgeless notes.
type LibrarianPassOpts struct {
	Scope  string
	MaxAge time.Duration // notes older than this with no edges → stale/archive
	Limit  int
	DryRun bool
	Status string // default archived
}

func init() {
	Registry = append(Registry, opLibrarianPass)
}

type librarianPassInput struct {
	Scope  string `json:"scope,omitempty"`
	MaxAge string `json:"max_age,omitempty"` // Go duration, e.g. 720h
	Limit  int    `json:"limit,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
	Status string `json:"status,omitempty"`
}

var opLibrarianPass = Op{
	Name: "librarian_pass",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in librarianPassInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		opts := LibrarianPassOpts{
			Scope:  in.Scope,
			Limit:  in.Limit,
			DryRun: in.DryRun,
			Status: in.Status,
		}
		if in.MaxAge != "" {
			d, err := time.ParseDuration(in.MaxAge)
			if err != nil {
				return "", fmt.Errorf("max_age: %w", err)
			}
			opts.MaxAge = d
		}
		return LibrarianPass(ctx, svc, opts)
	},
}

// LibrarianPass marks long-idle, edgeless knowledge.note artifacts via librarian stale.
// Safe/conservative: never merges or unlinks; only status transitions with Force.
func LibrarianPass(ctx context.Context, svc *Service, opts LibrarianPassOpts) (string, error) {
	opts = normalizeLibrarianPassOpts(opts)
	cutoff := time.Now().UTC().Add(-opts.MaxAge)
	labels := []string{kindLabelKnowledge}
	if opts.Scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+opts.Scope)
	}
	arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels, Limit: librarianListLimit})
	if err != nil {
		return "", err
	}
	var marked []string
	for _, art := range arts {
		if len(marked) >= opts.Limit {
			break
		}
		if !librarianPassCandidate(ctx, svc, art, cutoff) {
			continue
		}
		if opts.DryRun {
			marked = append(marked, art.ID)
			continue
		}
		if _, err := librarianStale(ctx, svc, librarianInput{ID: art.ID, Status: opts.Status}); err != nil {
			slog.WarnContext(ctx, "librarian pass stale failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		marked = append(marked, art.ID)
	}
	return formatLibrarianPassResult(opts.DryRun, marked), nil
}

func normalizeLibrarianPassOpts(opts LibrarianPassOpts) LibrarianPassOpts {
	if opts.MaxAge <= 0 {
		opts.MaxAge = defaultLibrarianMaxAge
	}
	if opts.Limit <= 0 {
		opts.Limit = librarianPassLimit
	}
	if opts.Status == "" {
		opts.Status = statusArchived
	}
	return opts
}

func librarianPassCandidate(ctx context.Context, svc *Service, art *parchment.Artifact, cutoff time.Time) bool {
	status := parchment.StatusFromLabels(art.Labels)
	if status == statusArchived || status == statusArchivedPrefixed || status == librarianDefaultStale {
		return false
	}
	ageAt := art.CreatedAt
	if ageAt.IsZero() {
		ageAt = art.InsertedAt
	}
	if ageAt.IsZero() {
		ageAt = art.UpdatedAt
	}
	if ageAt.After(cutoff) {
		return false
	}
	outE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
	inE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
	if len(outE) > 0 || len(inE) > 0 {
		return false
	}
	return !isIntentionalOrphan(art)
}

func formatLibrarianPassResult(dryRun bool, marked []string) string {
	if len(marked) == 0 {
		return "librarian pass: nothing to do"
	}
	verb := "marked"
	if dryRun {
		verb = "would mark"
	}
	return fmt.Sprintf("librarian pass: %s %d notes (%s)", verb, len(marked), strings.Join(marked, ", "))
}

// LibrarianTicker runs LibrarianPass on an interval (opt-in via NewLibrarianTicker).
type LibrarianTicker struct {
	svc      *Service
	interval time.Duration
	opts     LibrarianPassOpts
	stop     chan struct{}
}

// NewLibrarianTicker starts a background librarian pass. Call Stop on shutdown.
func NewLibrarianTicker(svc *Service, interval time.Duration, opts LibrarianPassOpts) *LibrarianTicker {
	lt := &LibrarianTicker{
		svc:      svc,
		interval: interval,
		opts:     opts,
		stop:     make(chan struct{}),
	}
	go lt.run()
	return lt
}

// Stop halts the background pass.
func (lt *LibrarianTicker) Stop() {
	close(lt.stop)
}

func (lt *LibrarianTicker) run() {
	lt.sweep()
	ticker := time.NewTicker(lt.interval)
	defer ticker.Stop()
	for {
		select {
		case <-lt.stop:
			return
		case <-ticker.C:
			lt.sweep()
		}
	}
}

func (lt *LibrarianTicker) sweep() {
	ctx := context.Background()
	out, err := LibrarianPass(ctx, lt.svc, lt.opts)
	if err != nil {
		slog.WarnContext(ctx, "librarian ticker", slog.Any(parchment.LogKeyError, err))
		return
	}
	slog.InfoContext(ctx, "librarian ticker", slog.String(logKeyResult, out))
}

// LibrarianIntervalFromEnv returns ticker interval from SCRIBE_LIBRARIAN_INTERVAL
// (Go duration). Empty or "0" disables the ticker.
func LibrarianIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SCRIBE_LIBRARIAN_INTERVAL"))
	if raw == "" || raw == "0" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		if hours, err2 := strconv.Atoi(raw); err2 == nil {
			return time.Duration(hours) * time.Hour
		}
		slog.WarnContext(context.Background(), "invalid SCRIBE_LIBRARIAN_INTERVAL", slog.String(logKeyValue, raw))
		return 0
	}
	return d
}
