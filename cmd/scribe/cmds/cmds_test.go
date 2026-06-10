package cmds_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/spf13/cobra"
)

// newDB returns a path to a fresh temp SQLite database for the test.
func newDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

// run builds a fresh root command wired to dbPath, executes args, and
// returns captured stdout. Tests must not call t.Parallel() — os.Stdout
// redirection is not goroutine-safe.
func run(t *testing.T, dbPath string, args ...string) string {
	t.Helper()

	cmds.DBPath = dbPath
	cmds.ConfigPath = ""
	t.Cleanup(func() {
		cmds.DBPath = ""
		cmds.ConfigPath = ""
	})

	root := &cobra.Command{Use: "scribe", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&cmds.DBPath, "db", dbPath, "")
	root.PersistentFlags().StringVar(&cmds.ConfigPath, "config", "", "")
	root.AddCommand(
		cmds.CreateCmd(),
		cmds.ShowCmd(),
		cmds.ListCmd(),
		cmds.SetCmd(),
		cmds.DeleteCmd(),
		cmds.TreeCmd(),
		cmds.BriefingCmd(),
		cmds.LinkCmd(),
		cmds.UnlinkCmd(),
		cmds.SectionCmd(),
		cmds.SearchCmd(),
		cmds.BriefCmd(),
		cmds.VacuumCmd(),
		cmds.OrphansCmd(),
		cmds.OverlapsCmd(),
		cmds.DfCmd(),
		cmds.SyncCmd(),
	)

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
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read output: %v", err)
	}
	r.Close()

	return buf.String()
}

// mustContain fails if output does not contain substr.
func mustContain(t *testing.T, output, substr string) {
	t.Helper()
	if !strings.Contains(output, substr) {
		t.Errorf("expected %q in output\ngot: %s", substr, output)
	}
}

// mustNotContain fails if output contains substr.
func mustNotContain(t *testing.T, output, substr string) {
	t.Helper()
	if strings.Contains(output, substr) {
		t.Errorf("did not expect %q in output\ngot: %s", substr, output)
	}
}
