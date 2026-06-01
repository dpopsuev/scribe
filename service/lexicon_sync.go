package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/dpopsuev/ordo/registry"
	parchment "github.com/dpopsuev/parchment"
)

const (
	lexKindRule  = "rule"
	lexKindSkill = "skill"

	// slog key constants — sloglint no-raw-keys.
	logKeyLexID    = "id"
	logKeyLexError = "error"
	logKeyLexCount = "count"
)

// SyncLexicon loads all rules and skills from the Ordo registry at lexRoot,
// bulk-upserts them into Parchment, and removes stale entries.
// Returns the count of artifacts synced.
func (s *Service) SyncLexicon(ctx context.Context, lexRoot string) (int, error) {
	store := s.Proto.Store()

	if err := ensureLexKindDefinition(ctx, store, lexKindRule); err != nil {
		slog.WarnContext(ctx, "sync-lexicon: cannot register rule kind", slog.Any(logKeyLexError, err))
	}
	if err := ensureLexKindDefinition(ctx, store, lexKindSkill); err != nil {
		slog.WarnContext(ctx, "sync-lexicon: cannot register skill kind", slog.Any(logKeyLexError, err))
	}

	reg := registry.New(lexRoot)
	sources, err := reg.Load()
	if err != nil {
		return 0, fmt.Errorf("load lexicon registry: %w", err)
	}

	var arts []*parchment.Artifact
	currentIDs := make(map[string]bool)

	for i := range sources {
		src := &sources[i]
		if !src.Enabled {
			continue
		}
		cfg, _ := registry.LoadLexiconConfig(src.LocalPath)
		priority := src.Priority
		if cfg != nil && cfg.Defaults.Priority > 0 {
			priority = cfg.Defaults.Priority
		}
		discovered := registry.DiscoverArtifacts(src.LocalPath, src.URL, priority)
		for j := range discovered {
			art := lexDiscoveredToArtifact(discovered[j], lexReadBody(discovered[j].Path))
			currentIDs[art.ID] = true
			arts = append(arts, art)
		}
	}

	if len(arts) > 0 {
		errs := store.BulkPut(ctx, arts)
		for i, e := range errs {
			if e != nil && i < len(arts) {
				slog.WarnContext(ctx, "sync-lexicon: bulk put failed",
					slog.String(logKeyLexID, arts[i].ID), slog.Any(logKeyLexError, e))
			}
		}
	}

	for _, scope := range []string{"global", "project"} {
		existing, err := store.List(ctx, parchment.Filter{Scope: scope})
		if err != nil {
			continue
		}
		for _, art := range existing {
			if art.Kind != lexKindRule && art.Kind != lexKindSkill {
				continue
			}
			if !currentIDs[art.ID] {
				_ = store.Delete(ctx, art.ID)
			}
		}
	}

	slog.InfoContext(ctx, "sync-lexicon: done", slog.Int(logKeyLexCount, len(arts)))
	return len(arts), nil
}

func ensureLexKindDefinition(ctx context.Context, store parchment.Store, kindName string) error {
	id := "DEF-" + kindName
	if _, err := store.Get(ctx, id); err == nil {
		return nil
	}
	prefix := strings.ToUpper(kindName)
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	return store.Put(ctx, &parchment.Artifact{
		ID:     id,
		Kind:   parchment.KindDefinition,
		Scope:  parchment.SchemaScope,
		Title:  kindName,
		Status: parchment.StatusActive,
		Extra: map[string]any{
			"prefix":         prefix,
			"code":           prefix,
			"family":         parchment.FamilyKnowledge,
			"default_status": parchment.StatusActive,
			"protected":      true,
			"skip_guards":    true,
		},
	})
}

func lexDiscoveredToArtifact(a registry.DiscoveredArtifact, body string) *parchment.Artifact {
	kind := lexKindRule
	if a.Type == lexKindSkill {
		kind = lexKindSkill
	}
	labels := lexAppendSourceLabel(a.Labels, a.Source)
	if a.AlwaysApply {
		labels = append(labels, "always")
	}
	now := time.Now().UTC()
	return &parchment.Artifact{
		ID:         lexArtifactID(kind, a.Source, a.Name),
		Kind:       kind,
		Scope:      "global",
		Title:      a.Name,
		Status:     parchment.StatusActive,
		Labels:     labels,
		CreatedAt:  now,
		UpdatedAt:  now,
		InsertedAt: now,
		Extra: map[string]any{
			"priority":     a.Priority,
			"source":       a.Source,
			"always_apply": a.AlwaysApply,
		},
		Sections: []parchment.Section{{Name: "content", Text: body}},
	}
}

func lexReadBody(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // operator-controlled path from Ordo registry
	if err != nil {
		return ""
	}
	content := string(data)
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---\n")
		if end >= 0 {
			content = strings.TrimSpace(content[4+end+5:])
		}
	}
	return content
}

func lexAppendSourceLabel(labels []string, source string) []string {
	out := make([]string, len(labels))
	copy(out, labels)
	if source != "" {
		out = append(out, "source:"+source)
	}
	return out
}

func lexArtifactID(kind, source, name string) string {
	key := kind + ":" + source + "/" + name
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("LDEF-%x", h[:4])
}
