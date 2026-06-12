package cmds_test

import (
	"testing"
)

func TestVacuum_ReportsNothingToVacuum(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "vacuum")
	mustContain(t, out, "nothing to vacuum")
}

func TestOrphans_RunsClean(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "orphans")
	if out == "" {
		t.Error("expected orphans output, got empty string")
	}
}

func TestOverlaps_RunsClean(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "overlaps")
	if out == "" {
		t.Error("expected overlaps output, got empty string")
	}
}
