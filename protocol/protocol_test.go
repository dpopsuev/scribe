package protocol_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
)

func openStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newProto(t *testing.T) (*protocol.Protocol, store.Store) {
	t.Helper()
	s := openStore(t)
	return protocol.New(s, nil, nil), s
}

func TestIsComponentLabel(t *testing.T) {
	good := []string{
		"locus:internal/arch",
		"scribe:protocol/protocol.go",
		"limes:internal/limesfile/limesfile.go",
		"my-project:src/pkg/foo",
		"a1:x/y",
	}
	bad := []string{
		"no-colon-here",
		"UPPER:path/foo",
		":path/foo",
		"project:",
		"project:noSlash",
		"",
	}
	for _, l := range good {
		if !protocol.IsComponentLabel(l) {
			t.Errorf("expected %q to be a valid component label", l)
		}
	}
	for _, l := range bad {
		if protocol.IsComponentLabel(l) {
			t.Errorf("expected %q to NOT be a valid component label", l)
		}
	}
}

func TestDetectOverlaps_NoOverlaps(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "active", Title: "A",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-2", Kind: "contract", Status: "active", Title: "B",
		Labels: []string{"locus:internal/mcp"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 0 {
		t.Errorf("expected 0 overlaps, got %d", report.TotalOverlaps)
	}
	if report.TotalScanned != 2 {
		t.Errorf("expected 2 scanned, got %d", report.TotalScanned)
	}
}

func TestDetectOverlaps_WithOverlaps(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "active", Title: "Contract A",
		Labels: []string{"locus:internal/arch", "locus:internal/mcp"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-2", Kind: "contract", Status: "active", Title: "Contract B",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-3", Kind: "contract", Status: "active", Title: "Contract C",
		Labels: []string{"scribe:protocol/protocol.go"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 1 {
		t.Errorf("expected 1 overlap, got %d", report.TotalOverlaps)
	}
	if report.TotalScanned != 3 {
		t.Errorf("expected 3 scanned, got %d", report.TotalScanned)
	}
	if report.Overlaps[0].Label != "locus:internal/arch" {
		t.Errorf("expected overlapping label locus:internal/arch, got %s", report.Overlaps[0].Label)
	}
	if len(report.Overlaps[0].Artifacts) != 2 {
		t.Errorf("expected 2 artifacts in overlap, got %d", len(report.Overlaps[0].Artifacts))
	}
}

func TestDetectOverlaps_ProjectFilter(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "active", Title: "A",
		Labels: []string{"locus:internal/arch", "scribe:mcp/server.go"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-2", Kind: "contract", Status: "active", Title: "B",
		Labels: []string{"locus:internal/arch", "scribe:mcp/server.go"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{Project: "scribe"})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 1 {
		t.Errorf("expected 1 overlap for project scribe, got %d", report.TotalOverlaps)
	}
	if report.Overlaps[0].Label != "scribe:mcp/server.go" {
		t.Errorf("expected scribe label overlap, got %s", report.Overlaps[0].Label)
	}
}

func TestComponentLabelGate_Blocks(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "draft", Title: "Gated",
		Sections: []model.Section{{Name: "specification", Text: "some spec"}},
	})

	results, err := p.SetField(ctx, []string{"CON-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("expected gate to block activation, but it succeeded")
	}
	if results[0].Error == "" {
		t.Error("expected error message from gate")
	}
}

func TestComponentLabelGate_Passes(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "draft", Title: "Labeled",
		Labels:   []string{"locus:internal/arch"},
		Sections: []model.Section{{Name: "specification", Text: "some spec"}},
	})

	results, err := p.SetField(ctx, []string{"CON-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("expected gate to pass, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_NoTriggerSection(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "draft", Title: "No trigger",
		Sections: []model.Section{{Name: "notes", Text: "just notes"}},
	})

	results, err := p.SetField(ctx, []string{"CON-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should not fire for non-trigger sections, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_DisabledByDefault(t *testing.T) {
	os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "CON-1", Kind: "contract", Status: "draft", Title: "Ungated",
		Sections: []model.Section{{Name: "specification", Text: "spec"}},
	})

	results, err := p.SetField(ctx, []string{"CON-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should not fire when env var is unset, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_NonContract(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "SPR-1", Kind: "sprint", Status: "draft", Title: "Sprint",
		Sections: []model.Section{{Name: "specification", Text: "spec"}},
	})

	results, err := p.SetField(ctx, []string{"SPR-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should only apply to contracts, got error: %s", results[0].Error)
	}
}
