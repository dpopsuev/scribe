package cmds_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
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

// setupPreMigrationDB creates a temp SQLite simulating a pre-ECS database:
// artifacts have Kind/Scope/Status columns populated but no rows in
// artifact_labels. Uses raw SQL to bypass syncSystemFields in Store.Put.
func setupPreMigrationDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pre-ecs.sqlite")

	// Open via Store first so the schema (DDL) is initialized.
	s, err := parchment.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_ = s.Close()

	// Write directly via database/sql — no syncSystemFields, no artifact_labels rows.
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer conn.Close() //nolint:errcheck // test teardown
	rows := []struct{ id, kind, scope, status, title string }{
		{"TST-TSK-1", "task", "test", "draft", "old task 1"},
		{"TST-TSK-2", "task", "test", "active", "old task 2"},
		{"TST-SPC-1", "spec", "test", "draft", "old spec"},
	}
	for _, r := range rows {
		_, err := conn.Exec(
			`INSERT INTO artifacts (uid, id, kind, scope, status, title, labels, created_at, updated_at, inserted_at)
			 VALUES (?, ?, ?, ?, ?, ?, '[]', datetime('now'), datetime('now'), datetime('now'))`,
			r.id, r.id, r.kind, r.scope, r.status, r.title,
		)
		if err != nil {
			t.Fatalf("raw insert %s: %v", r.id, err)
		}
	}
	return dbPath
}
