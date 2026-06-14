package migrations

import (
	"context"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// migrateInvestigationCase renames the tautological kind investigation.investigation
// to investigation.case and removes its stale LDEF schema artifact.
func migrateInvestigationCase(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}
	const oldKind = "investigation.investigation"
	const newKind = "investigation.case"
	var renamed, pruned int
	for _, art := range all {
		if art.Label(parchment.LabelPrefixKind) == oldKind {
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
		if art.ID == "LDEF-kind:"+oldKind {
			if err := proto.Store().Delete(ctx, art.ID); err != nil {
				return fmt.Errorf("delete stale LDEF %s: %w", art.ID, err)
			}
			pruned++
		}
	}
	if renamed > 0 {
		fmt.Printf("  relabeled %d artifacts (investigation.investigation → investigation.case)\n", renamed)
	}
	if pruned > 0 {
		fmt.Printf("  deleted stale LDEF-kind:investigation.investigation\n")
	}
	return nil
}
