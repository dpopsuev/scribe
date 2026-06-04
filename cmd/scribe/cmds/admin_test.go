package cmds_test

import (
	"testing"
)

func TestBrief_RunsWithEmptyStore(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "brief")
	if out == "" {
		t.Error("expected brief output, got empty string")
	}
}

func TestDf_RunsWithEmptyStore(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "df")
	if out == "" {
		t.Error("expected df output, got empty string")
	}
}

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
