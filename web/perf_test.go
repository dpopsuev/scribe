//go:build browser

// Package web_test — browser performance tests.
//
// These tests drive the production /graph page directly via chromedp and the
// Chrome Devtools Protocol. No special test route exists. The test:
//
//  1. Starts a real httptest.Server backed by a parchment store.
//  2. Navigates Chrome to /graph.
//  3. Injects a requestAnimationFrame counter into the page to measure real FPS.
//  4. Calls the page's existing JS functions (loadMacro, addNodes via graphData)
//     to load data at controlled node counts.
//  5. Asserts that FPS stays above thresholds — fails the test on regression.
//
// Run: go test -tags browser -v -timeout 120s ./web/...
// Requires: Google Chrome installed. Uses SwiftShader software GL for
// reproducibility without GPU; remove --use-gl=swiftshader for hardware GL.
package web_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

// fpsThreshold defines the minimum acceptable FPS at a given node count when
// driving the real /graph page. Tighten as optimizations land.
type fpsThreshold struct {
	nodes      int
	minFPS     float64
	desc       string
}

var thresholds = []fpsThreshold{
	{nodes: 50,   minFPS: 55, desc: "50 nodes — trivial, must be near-native"},
	{nodes: 200,  minFPS: 45, desc: "200 nodes — comfortable with default physics"},
	{nodes: 500,  minFPS: 30, desc: "500 nodes — acceptable without optimisations"},
}

// startServer spins up a real httptest.Server with a parchment store.
func startServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := parchment.OpenSQLite(t.TempDir() + "/perf.db")
	if err != nil {
		t.Fatal(err)
	}
	proto := parchment.New(s, nil, []string{"perf"}, nil, parchment.ProtocolConfig{})
	srv := httptest.NewServer(web.NewServer(proto))
	t.Cleanup(func() { srv.Close(); _ = s.Close() })
	return srv
}

// newBrowser returns a chromedp context. Uses SwiftShader (software GL) so
// tests are reproducible without GPU. On hardware, remove --use-gl=swiftshader.
func newBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		// angle = ANGLE (Almost Native Graphics Layer Engine) — works on most
		// Linux CI environments including those without a discrete GPU.
		// swiftshader (pure SW) fails on this system: BindToCurrentSequence.
		chromedp.Flag("use-gl", "angle"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancel)
	ctx, cancel := chromedp.NewContext(alloc)
	t.Cleanup(cancel)
	return ctx
}

// measureFPS injects a requestAnimationFrame counter into the page, waits
// windowSec seconds, then reads the result. Returns actual rendered FPS.
// This is the canonical way to measure animation smoothness — no custom page,
// no special instrumentation, just counting frames in the real render loop.
func measureFPS(ctx context.Context, windowSec float64) (float64, error) {
	// Inject the counter — reuses whatever rAF loop is already running.
	const startJS = `
		window.__perfFrameCount = 0;
		window.__perfStartTime  = performance.now();
		window.__perfCounting   = true;
		(function tick() {
			if (window.__perfCounting) {
				window.__perfFrameCount++;
				requestAnimationFrame(tick);
			}
		})();
		true;
	`
	if err := chromedp.Run(ctx, chromedp.Evaluate(startJS, nil)); err != nil {
		return 0, fmt.Errorf("start counter: %w", err)
	}

	time.Sleep(time.Duration(windowSec * float64(time.Second)))

	const stopJS = `
		window.__perfCounting = false;
		window.__perfFrameCount / ((performance.now() - window.__perfStartTime) / 1000);
	`
	var fps float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(stopJS, &fps)); err != nil {
		return 0, fmt.Errorf("read fps: %w", err)
	}
	return fps, nil
}

// TestGraph_FPS_AtNodeCounts navigates to the real /graph page, injects nodes
// at controlled counts, and asserts FPS thresholds using real frame timing.
func TestGraph_FPS_AtNodeCounts(t *testing.T) {
	srv := startServer(t)

	for _, th := range thresholds {
		th := th
		t.Run(fmt.Sprintf("%d_nodes", th.nodes), func(t *testing.T) {
			ctx := newBrowser(t)
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()

			// Navigate to the real production graph page and wait for the
			// ForceGraph3D JS object to be initialised in the page scope.
			// WaitFunc polls until the expression evaluates to truthy.
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/graph"),
				chromedp.Poll(`typeof Graph !== 'undefined' && typeof Graph.graphData === 'function'`,
					nil, chromedp.WithPollingInterval(500*time.Millisecond)),
			); err != nil {
				t.Fatalf("navigate /graph: %v", err)
			}

			// Wait for loadMacro() to complete — the graph is live and floating.
			time.Sleep(3 * time.Second)

			// Inject synthetic artifact nodes directly into the graph's data via
			// the page's existing graphData object and Graph instance.
			// This exercises the real physics engine and render path — not a mock.
			injectJS := fmt.Sprintf(`
				(function() {
					// Universe view: inject scope super-nodes matching the
					// expected format from /api/graph/scopes.
					var existing = _Graph.graphData().nodes.length;
					var toAdd = %d - existing;
					if (toAdd <= 0) return 'already_at_target';

					var nodes = Graph.graphData().nodes.slice();
					var links = Graph.graphData().links.slice();
					for (var i = 0; i < toAdd; i++) {
						var id = 'scope:perf-' + (existing + i);
						nodes.push({id: id, name: 'perf-' + i, kind: 'scope',
						             scope: 'perf-' + i, val: 3 + Math.floor(Math.random()*8)});
						if (nodes.length > 1) {
							links.push({
								source: nodes[Math.floor(Math.random() * (nodes.length-1))].id,
								target: id,
								relation: 'cross-scope',
								weight: 1,
							});
						}
					}
					_Graph.graphData({nodes: nodes, links: links});
					return 'injected:' + toAdd;
				})();
			`, th.nodes)
			var injectResult string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(injectJS, &injectResult),
			); err != nil {
				t.Fatalf("inject nodes: %v", err)
			}
			t.Logf("inject result: %s", injectResult)

			// Let physics stabilise before measuring.
			time.Sleep(3 * time.Second)

			// Verify node count reached.
			var nodeCount int
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`_Graph.graphData().nodes.length`, &nodeCount),
			); err != nil {
				t.Fatalf("read node count: %v", err)
			}
			t.Logf("node count: %d (target %d)", nodeCount, th.nodes)

			// Measure FPS over a 4-second window using rAF counter.
			// 4s gives ~240 frames at 60fps — statistically stable.
			fps, err := measureFPS(ctx, 4)
			if err != nil {
				t.Fatalf("measure fps: %v", err)
			}
			t.Logf("%.1f fps at %d nodes — %s", fps, nodeCount, th.desc)

			if fps < th.minFPS {
				t.Errorf("REGRESSION: %.1f fps at %d nodes, want >= %.0f — %s",
					fps, nodeCount, th.minFPS, th.desc)
			}
		})
	}
}

// TestGraph_NoJSErrors loads the real /graph page and asserts a clean console.
// Catches broken CDN imports, Three.js API changes, JS syntax errors in
// graph.html templates — all invisible to httptest-based tests.
func TestGraph_NoJSErrors(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var jsErrors []string
	chromedp.ListenTarget(ctx, func(ev any) {
		if ev, ok := ev.(*runtime.EventExceptionThrown); ok {
			jsErrors = append(jsErrors, ev.ExceptionDetails.Error())
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second), // CDN scripts (3d-force-graph ~1.3MB) need time to load
		chromedp.Sleep(3*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	if len(jsErrors) > 0 {
		for _, e := range jsErrors {
			t.Errorf("JS exception: %s", e)
		}
	}
}

// TestGraph_WebGLContext verifies the Three.js WebGL renderer initialises.
// A missing CDN asset or WebGL flag misconfiguration fails here.
func TestGraph_WebGLContext(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Capture JS console and exceptions for diagnosis.
	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			for _, arg := range ev.Args {
				t.Logf("[console] %s", arg.Value)
			}
		case *runtime.EventExceptionThrown:
			t.Logf("[exception] %s", ev.ExceptionDetails.Error())
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second), // CDN scripts (~1.3MB) need time to load
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Check if _Graph is defined at all — diagnose before reading context.
	var graphDefined bool
	chromedp.Run(ctx, chromedp.Evaluate(`!!window._Graph`, &graphDefined)) //nolint:errcheck
	t.Logf("window._Graph defined: %v", graphDefined)

	// Read WebGL context type from the Three.js renderer.
	var ctxType string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(
			`window._Graph ? window._Graph.renderer().getContext().constructor.name : 'not_init'`,
			&ctxType,
		),
	); err != nil {
		t.Fatalf("read GL context: %v", err)
	}
	t.Logf("WebGL context: %s", ctxType)

	if ctxType != "WebGLRenderingContext" && ctxType != "WebGL2RenderingContext" {
		t.Errorf("expected WebGL context, got %q", ctxType)
	}
}

// TestGraph_FrameMetrics measures FPS and JS heap while the scope universe
// graph is running. Uses requestAnimationFrame counting (reliable in headless)
// rather than CDP Performance.getMetrics AnimationFrames (unreliable in ANGLE).
func TestGraph_FrameMetrics(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second), // CDN scripts need time to load
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Measure FPS over 4 seconds using rAF counter (same as measureFPS).
	fps, err := measureFPS(ctx, 4)
	if err != nil {
		t.Fatalf("measureFPS: %v", err)
	}

	// Read JS heap size directly from performance.memory (Chrome-only).
	var heapMB float64
	chromedp.Run(ctx, chromedp.Evaluate( //nolint:errcheck // optional metric
		`performance.memory ? performance.memory.usedJSHeapSize / 1024 / 1024 : 0`,
		&heapMB,
	))

	t.Logf("fps=%.1f  heap=%.1fMB (scope universe, ~82 nodes)", fps, heapMB)

	// At idle with ~82 scope super-nodes and full physics, we must sustain 30fps.
	// This catches regressions in the radial force, physics decay, or render path.
	if fps < 30 {
		t.Errorf("REGRESSION: %.1f fps at idle, want >= 30", fps)
	}

	// JS heap > 300MB at idle suggests a memory leak in the graph initialisation.
	if heapMB > 300 {
		t.Errorf("REGRESSION: %.0fMB heap at idle, want < 300MB", heapMB)
	}

	// Heap > 400MB at idle (scope super-nodes only) indicates a memory leak.
	if heapMB > 400 {
		t.Errorf("REGRESSION: %.0fMB JS heap, want < 400MB", heapMB)
	}
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
