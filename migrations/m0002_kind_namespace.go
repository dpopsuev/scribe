package migrations

import (
	"context"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// kindRenames maps old flat kind names to new dot-namespaced kind names.
// This table is append-only and permanent — do not remove entries.
var kindRenames = map[string]string{
	"task":           "effort.task",
	"goal":           "effort.goal",
	"campaign":       "effort.campaign",
	"bug":            "intent.bug",
	"spec":           "intent.spec",
	"decision":       "intent.decision",
	"need":           "intent.need",
	"note":           "knowledge.note",
	"concept":        "knowledge.concept",
	"source":         "knowledge.source",
	"journal":        "knowledge.journal",
	"context":        "knowledge.context",
	"config":         "support.config",
	"doc":            "support.doc",
	"mirror":         "support.mirror",
	"paragraph":      "support.paragraph",
	"ref":            "support.ref",
	"rule":           "support.rule",
	"section":        "support.section",
	"template":       "support.template",
	"cause":          "investigation.cause",
	"investigation":  "investigation.investigation",
	"observation":    "investigation.observation",
	"code-interface": "code.interface",
	"code-test":      "code.test",
	"ctx-session":    "ctx.session",
	"ctx-tool-call":  "ctx.tool-call",
	"ctx-turn":       "ctx.turn",
}

// migrateKindNamespace renames flat kind labels to dot-namespaced format and
// removes stale LDEF-kind:X schema artifacts whose IDs encode the old name.
// The migration is idempotent: artifacts already using namespaced kinds are skipped.
func migrateKindNamespace(ctx context.Context, proto *parchment.Protocol) error {
	// Use the raw Store to reach _schema artifacts that Protocol.ListArtifacts excludes.
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var renamed, pruned int
	for _, art := range all {
		current := art.Label(parchment.LabelPrefixKind)

		// Rename user artifact labels.
		if newKind, ok := kindRenames[current]; ok {
			newLabels := make([]string, 0, len(art.Labels))
			for _, l := range art.Labels {
				if strings.HasPrefix(l, parchment.LabelPrefixKind) {
					newLabels = append(newLabels, parchment.LabelPrefixKind+newKind)
				} else {
					newLabels = append(newLabels, l)
				}
			}
			art.Labels = newLabels
			if err := proto.Store().Put(ctx, art); err != nil {
				return fmt.Errorf("relabel %s: %w", art.ID, err)
			}
			renamed++
			continue
		}

		// Delete stale LDEF-kind:X schema artifacts for renamed kinds.
		const ldefPrefix = "LDEF-kind:"
		if strings.HasPrefix(art.ID, ldefPrefix) {
			oldName := art.ID[len(ldefPrefix):]
			if _, isOld := kindRenames[oldName]; isOld {
				if err := proto.Store().Delete(ctx, art.ID); err != nil {
					return fmt.Errorf("delete stale LDEF %s: %w", art.ID, err)
				}
				pruned++
			}
		}
	}

	if renamed > 0 {
		fmt.Printf("  relabeled %d artifacts (flat kind → namespaced)\n", renamed)
	}
	if pruned > 0 {
		fmt.Printf("  deleted %d stale LDEF-kind schema artifacts\n", pruned)
	}
	return nil
}
