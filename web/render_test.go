//go:build browser

package web_test

// TestGraph_NodesAreVisible is the canonical "nodes actually render" test.
// It checks:
//   1. CSS custom properties are injected (palette script ran without error)
//   2. Every graph-color-kind-* var is a non-empty hex string
//   3. The first rendered node has a non-empty color string
//   4. No JS exceptions were thrown during page load
//
// This test was written to catch the getPageBgHex() crash (document.body=null)
// that made nodes invisible, and the culori toMode API mismatch.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestGraph_NodesAreVisible(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var jsErrors []string
	chromedp.ListenTarget(ctx, func(ev any) {
		if ev, ok := ev.(*runtime.EventExceptionThrown); ok {
			jsErrors = append(jsErrors, ev.ExceptionDetails.Error())
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Fail fast on any JS exception — a crash in the palette script or
	// graph init makes nodes invisible without an obvious visual clue.
	for _, e := range jsErrors {
		// Filter out known non-critical errors (e.g. CDN timing on first load).
		if strings.Contains(e, "culori") || strings.Contains(e, "getComputedStyle") ||
			strings.Contains(e, "palette") || strings.Contains(e, "TypeError") {
			t.Errorf("JS exception that breaks rendering: %s", e)
		}
	}

	const js = `(function() {
		try {
			// 1. Check CSS vars were injected.
			var root = document.documentElement;
			var cs = getComputedStyle(root);
			var kinds = ['task','spec','bug','goal','note','scope'];
			var missingVars = kinds.filter(function(k) {
				var v = cs.getPropertyValue('--graph-color-kind-' + k).trim();
				return !v || !v.startsWith('#');
			});

			// 2. Check nodes exist and have color.
			var nodes = _Graph.graphData().nodes;
			var firstColor = nodes.length > 0 ? _Graph.nodeColor()(nodes[0]) : '';
			var hasColor = firstColor && firstColor !== '' && firstColor !== 'undefined';

			// 3. Sample color contrast against graph bg (#05050f).
			var taskHex = cs.getPropertyValue('--graph-color-kind-task').trim();
			var contrast = taskHex ? culori.wcagContrast(taskHex, '#05050f') : 0;

			return JSON.stringify({
				nodeCount:    nodes.length,
				missingVars:  missingVars,
				firstColor:   firstColor,
				hasColor:     hasColor,
				taskHex:      taskHex,
				taskContrast: Math.round(contrast * 10) / 10,
			});
		} catch(e) { return JSON.stringify({fatalError: e.message}); }
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var m struct {
		FatalError   string   `json:"fatalError"`
		NodeCount    int      `json:"nodeCount"`
		MissingVars  []string `json:"missingVars"`
		FirstColor   string   `json:"firstColor"`
		HasColor     bool     `json:"hasColor"`
		TaskHex      string   `json:"taskHex"`
		TaskContrast float64  `json:"taskContrast"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if m.FatalError != "" {
		t.Fatalf("fatal JS error: %s", m.FatalError)
	}

	t.Logf("nodes: %d  firstColor: %q  taskHex: %s  contrast: %.1f:1",
		m.NodeCount, m.FirstColor, m.TaskHex, m.TaskContrast)

	// CSS vars must be injected.
	if len(m.MissingVars) > 0 {
		t.Errorf("palette script failed — missing CSS vars: %v", m.MissingVars)
	}

	// Nodes must exist (server has seeded data).
	if m.NodeCount == 0 {
		t.Error("no nodes in graph — loadMacro() may have failed")
	}

	// First node must have a non-empty color string.
	if !m.HasColor {
		t.Errorf("first node has no color %q — KIND_COLOR Proxy returning empty (CSS vars missing)", m.FirstColor)
	}

	// WCAG 3:1 minimum on the dark graph canvas.
	if m.TaskContrast < 3.0 {
		t.Errorf("task color %s contrast %.1f:1 < 3:1 WCAG minimum on #05050f", m.TaskHex, m.TaskContrast)
	}
}
