package cmds_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/spf13/cobra"
)

// runMigrate builds a root command that includes MigrateCmd and executes args.
func runMigrate(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	cmds.DBPath = dbPath
	cmds.ConfigPath = ""
	t.Cleanup(func() { cmds.DBPath = ""; cmds.ConfigPath = "" })

	root := &cobra.Command{Use: "scribe", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&cmds.DBPath, "db", dbPath, "")
	root.PersistentFlags().StringVar(&cmds.ConfigPath, "config", "", "")
	root.AddCommand(cmds.MigrateCmd())
	root.SetArgs(args)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	_ = root.Execute()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func TestMigrateLabelsCLI_DryRun(t *testing.T) {
	// Given: a SQLite DB with artifacts that have Kind/Scope/Status fields
	//        but no system labels.
	// When:  scribe migrate labels --dry-run
	// Then:  reports count of artifacts that would be migrated, writes nothing.
	db := setupPreMigrationDB(t)

	out := runMigrate(t, db, "migrate", "labels", "--dry-run")

	mustContain(t, out, "dry-run")
	mustContain(t, out, "3 total artifacts")
	mustContain(t, out, "0 already have kind: label")
	mustContain(t, out, "3 would be migrated")

	// Verify nothing was written.
	s, _ := parchment.OpenSQLite(db)
	defer s.Close() //nolint:errcheck // test teardown
	arts, _ := s.List(context.Background(), parchment.Filter{})
	for _, art := range arts {
		if slices.Contains(art.Labels, "kind:task") {
			t.Error("dry-run must not write any labels")
		}
	}
}

func TestMigrateLabelsCLI_Apply(t *testing.T) {
	// Given: pre-ECS artifacts without system labels.
	// When:  scribe migrate labels (no --dry-run).
	// Then:  reports migrated count; all artifacts now carry kind:/scope:/status: labels.
	db := setupPreMigrationDB(t)

	out := runMigrate(t, db, "migrate", "labels")

	mustContain(t, out, "migrated 3 artifacts")
	mustContain(t, out, "3 now carry system labels")

	// Verify labels are present.
	s, _ := parchment.OpenSQLite(db)
	defer s.Close() //nolint:errcheck // test teardown
	ctx := context.Background()
	arts, _ := s.List(ctx, parchment.Filter{})
	for _, art := range arts {
		if !slices.Contains(art.Labels, "kind:"+art.Kind) {
			t.Errorf("artifact %s missing kind:%s label after migration", art.ID, art.Kind)
		}
		if !slices.Contains(art.Labels, "scope:"+art.Scope) {
			t.Errorf("artifact %s missing scope:%s label after migration", art.ID, art.Scope)
		}
	}
}

func TestMigrateLabelsCLI_Idempotent(t *testing.T) {
	// Given: migration run twice on the same DB.
	// Then:  second run reports 0 migrated; no label duplicates.
	db := setupPreMigrationDB(t)

	runMigrate(t, db, "migrate", "labels")
	out2 := runMigrate(t, db, "migrate", "labels")

	mustContain(t, out2, "migrated 0 artifacts")

	s, _ := parchment.OpenSQLite(db)
	defer s.Close() //nolint:errcheck // test teardown
	arts, _ := s.List(context.Background(), parchment.Filter{})
	for _, art := range arts {
		counts := map[string]int{}
		for _, l := range art.Labels {
			counts[l]++
		}
		for label, n := range counts {
			if n > 1 {
				t.Errorf("artifact %s: label %q duplicated %d times", art.ID, label, n)
			}
		}
	}
}

func TestMigrateLabelsCLI_PostMigration_ScribeBoots(t *testing.T) {
	// Simulator E2E: migrate labels → boot Protocol → list by kind works.
	// This simulates the "migrate, validate, start" operator workflow.
	db := setupPreMigrationDB(t)

	// Step 1: migrate.
	out := runMigrate(t, db, "migrate", "labels")
	mustContain(t, out, "migrated 3 artifacts")

	// Step 2: boot Protocol (simulates service start).
	s, err := parchment.OpenSQLite(db)
	if err != nil {
		t.Fatalf("open post-migration DB: %v", err)
	}
	defer s.Close() //nolint:errcheck // test teardown
	p := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	// Step 3: list by kind — must find migrated artifacts via label.
	arts, err := p.ListArtifacts(ctx, parchment.ListInput{Kind: "task", Scope: "test"})
	if err != nil {
		t.Fatalf("list after migration: %v", err)
	}
	if len(arts) != 2 {
		t.Errorf("expected 2 tasks post-migration, got %d", len(arts))
	}
}

// setupPreMigrationDB creates a temp SQLite with 3 artifacts seeded via direct
// Put (bypassing Protocol, so no system labels are stamped) — simulating
// the pre-ECS state of a real database.
func setupPreMigrationDB(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "pre-ecs.sqlite")
	s, err := parchment.OpenSQLite(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	for _, art := range []*parchment.Artifact{
		{ID: "TST-TSK-1", Kind: "task", Scope: "test", Status: "draft", Title: "old task 1"},
		{ID: "TST-TSK-2", Kind: "task", Scope: "test", Status: "active", Title: "old task 2"},
		{ID: "TST-SPC-1", Kind: "spec", Scope: "test", Status: "draft", Title: "old spec"},
	} {
		if err := s.Put(ctx, art); err != nil {
			t.Fatalf("seed %s: %v", art.ID, err)
		}
	}
	_ = s.Close()
	return db
}
