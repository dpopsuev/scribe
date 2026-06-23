package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// ChangeKind classifies what changed during re-materialization.
type ChangeKind int

const (
	ChangeNone    ChangeKind = iota // content unchanged
	ChangeCreated                   // new artifact
	ChangeUpdated                   // content hash differs
)

func (c ChangeKind) String() string {
	switch c {
	case ChangeCreated:
		return "created"
	case ChangeUpdated:
		return "updated"
	default:
		return "unchanged"
	}
}

// ChangeRecord captures the result of re-materializing a single artifact.
type ChangeRecord struct {
	ID         string     `json:"id"`
	Change     ChangeKind `json:"change"`
	OldHash    string     `json:"old_hash,omitempty"`
	NewHash    string     `json:"new_hash,omitempty"`
	ChangeDesc string     `json:"change_desc,omitempty"`
}

// RematerializeResult summarizes a batch re-materialization.
type RematerializeResult struct {
	Total     int            `json:"total"`
	Created   int            `json:"created"`
	Updated   int            `json:"updated"`
	Unchanged int            `json:"unchanged"`
	Changes   []ChangeRecord `json:"changes,omitempty"`
}

// Rematerialize persists a batch of artifacts, detecting which ones are new,
// changed, or unchanged compared to their stored versions. It stamps
// content_hash and materialized_at on every artifact.
func Rematerialize(ctx context.Context, store parchment.Store, arts []*parchment.Artifact) *RematerializeResult {
	result := &RematerializeResult{Total: len(arts)}

	for _, art := range arts {
		StampContentHash(art)
		newHash := art.Extra["content_hash"].(string)

		existing, err := store.Get(ctx, art.ID)
		if err != nil {
			if err := store.Put(ctx, art); err != nil {
				slog.WarnContext(ctx, "rematerialize: put failed",
					slog.String("id", art.ID), slog.Any("error", err)) //nolint:sloglint // domain-specific
				continue
			}
			result.Created++
			result.Changes = append(result.Changes, ChangeRecord{
				ID:      art.ID,
				Change:  ChangeCreated,
				NewHash: newHash,
			})
			continue
		}

		oldHash, _ := existing.Extra["content_hash"].(string)
		if oldHash == newHash {
			art.Extra["materialized_at"] = time.Now().UTC().Format(time.RFC3339)
			if err := store.Put(ctx, art); err != nil {
				slog.WarnContext(ctx, "rematerialize: touch failed",
					slog.String("id", art.ID), slog.Any("error", err)) //nolint:sloglint // domain-specific
			}
			result.Unchanged++
			continue
		}

		desc := diffDescription(existing, art)
		if err := store.Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "rematerialize: update failed",
				slog.String("id", art.ID), slog.Any("error", err)) //nolint:sloglint // domain-specific
			continue
		}
		result.Updated++
		result.Changes = append(result.Changes, ChangeRecord{
			ID:         art.ID,
			Change:     ChangeUpdated,
			OldHash:    oldHash,
			NewHash:    newHash,
			ChangeDesc: desc,
		})
	}

	if result.Created > 0 || result.Updated > 0 {
		slog.InfoContext(ctx, "rematerialize",
			slog.Int("created", result.Created),     //nolint:sloglint // domain-specific
			slog.Int("updated", result.Updated),     //nolint:sloglint // domain-specific
			slog.Int("unchanged", result.Unchanged)) //nolint:sloglint // domain-specific
	}

	return result
}

func diffDescription(prev, curr *parchment.Artifact) string {
	var parts []string
	if prev.Title != curr.Title {
		parts = append(parts, "title")
	}
	if len(prev.Sections) != len(curr.Sections) {
		parts = append(parts, "sections")
	} else {
		for i := range prev.Sections {
			if i >= len(curr.Sections) || prev.Sections[i].Name != curr.Sections[i].Name || prev.Sections[i].Text != curr.Sections[i].Text {
				parts = append(parts, "sections")
				break
			}
		}
	}
	if len(prev.Labels) != len(curr.Labels) {
		parts = append(parts, "labels")
	}
	if len(parts) == 0 {
		return sectionContent
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result = fmt.Sprintf("%s, %s", result, parts[i])
	}
	return result
}
