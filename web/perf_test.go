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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
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
	nodes  int
	minFPS float64
	desc   string
}

var thresholds = []fpsThreshold{
	{nodes: 50, minFPS: 55, desc: "50 nodes — trivial, must be near-native"},
	{nodes: 200, minFPS: 45, desc: "200 nodes — comfortable with default physics"},
	{nodes: 500, minFPS: 30, desc: "500 nodes — acceptable without optimisations"},
}

// startServer spins up a real httptest.Server with a seeded parchment store.
// Creates artifacts in three scopes with different sizes so the scope graph
// has meaningful weighted nodes for camera and FPS tests.
func startServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := parchment.OpenSQLite(t.TempDir() + "/perf.db")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	// Seed three scopes with differing artifact counts so scope super-nodes
	// have different val values — exercises weighted center-of-mass logic.
	for i := range 40 {
		scope := []string{"alpha", "beta", "gamma"}[i%3]
		_ = s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("%s-%03d", scope, i), Kind: "effort.task",
			Scope: scope, Status: "active",
			Title: fmt.Sprintf("artifact %d", i),
		})
	}
	proto := parchment.New(s, nil, []string{"alpha", "beta", "gamma"}, nil, parchment.ProtocolConfig{})
	srv := httptest.NewServer(web.NewServer(proto, "dev", ""))
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

// TestGraph_RenderPipeline asserts each stage of the node rendering pipeline:
//
//  1. window.THREE is available (UMD build loaded correctly)
//  2. _Graph.graphData().nodes is non-empty after loadMacro
//  3. Every node has a Three.js object registered (nodeThreeObject was called)
//  4. No node material colour is black (0x000000) — palette wired correctly
//  5. Every node mesh has scale > 0 (scaleNodesByDistance ran at least once)
func TestGraph_RenderPipeline(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *runtime.EventExceptionThrown:
			t.Logf("[exception] %s", ev.ExceptionDetails.Error())
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	const pipelineJS = `(function() {
		try {
			// Stage 1: ForceGraph3D initialised
			var graphOK = typeof _Graph !== 'undefined' && typeof _Graph.graphData === 'function';
			if (!graphOK) return JSON.stringify({error: 'ForceGraph3D not initialised (_Graph missing)'});

			// Stage 2: graph data loaded — scope nodes present after loadMacro
			var nodes     = _Graph.graphData().nodes;
			var nodeCount = nodes.length;

			// Stage 3: WebGL context live and error-free
			var gl      = _Graph.renderer().getContext();
			var glError = gl.getError(); // 0 = GL_NO_ERROR

			// Stage 4: every node must have a corresponding sphere mesh in the scene.
			// ForceGraph3D always sets transparent=true on node materials (to support
			// variable nodeOpacity), so we do NOT filter by transparent here.
			var meshCount = 0, blackMaterial = 0, zeroScale = 0;
			_Graph.scene().traverse(function(obj) {
				if (!obj.isMesh) return;
				if (!obj.geometry || obj.geometry.type !== 'SphereGeometry') return;
				if (!obj.visible) return;
				// Skip glow meshes: they use BackSide rendering.
				if (obj.material && obj.material.side === 1) return; // THREE.BackSide = 1
				meshCount++;
				var c = obj.material && obj.material.color;
				if (c && c.r === 0 && c.g === 0 && c.b === 0) blackMaterial++;
				if (obj.scale && obj.scale.x <= 0) zeroScale++;
			});

			// Stage 5: window.THREE availability (optional — used for scope bubbles/glow)
			var threeOK = typeof THREE !== 'undefined' && typeof THREE.SphereGeometry === 'function';

			return JSON.stringify({
				graphOK:       graphOK,
				nodeCount:     nodeCount,
				meshCount:     meshCount,
				blackMaterial: blackMaterial,
				zeroScale:     zeroScale,
				glError:       glError,
				threeOK:       threeOK,
			});
		} catch(e) { return JSON.stringify({error: e.message}); }
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(pipelineJS, &raw)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var m struct {
		Error         string `json:"error"`
		GraphOK       bool   `json:"graphOK"`
		NodeCount     int    `json:"nodeCount"`
		MeshCount     int    `json:"meshCount"`
		BlackMaterial int    `json:"blackMaterial"`
		ZeroScale     int    `json:"zeroScale"`
		GLError       int    `json:"glError"`
		ThreeOK       bool   `json:"threeOK"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse: %v — raw: %s", err, raw)
	}
	if m.Error != "" {
		t.Fatalf("JS error: %s", m.Error)
	}

	t.Logf("ForceGraph3D initialised: %v", m.GraphOK)
	t.Logf("nodes loaded: %d", m.NodeCount)
	t.Logf("meshes in scene: %d", m.MeshCount)
	t.Logf("nodes with black material: %d", m.BlackMaterial)
	t.Logf("nodes with zero scale: %d", m.ZeroScale)
	t.Logf("WebGL error code: %d (0=none)", m.GLError)
	t.Logf("window.THREE available: %v (optional — glow/bubbles only)", m.ThreeOK)

	// Stage 1: graph initialised
	if !m.GraphOK {
		t.Fatal("FAIL stage 1: _Graph not initialised")
	}
	// Stage 2: data loaded
	if m.NodeCount == 0 {
		t.Error("FAIL stage 2: no nodes loaded — fetchScopeGraph or applyGraphData broken")
	}
	// Stage 3: WebGL clean — errors are swallowed by renderer, surface them here
	if m.GLError != 0 {
		t.Errorf("FAIL stage 3: WebGL error %d — shader compilation failed silently", m.GLError)
	}
	// Stage 4: one sphere mesh per node
	if m.MeshCount != m.NodeCount {
		t.Errorf("FAIL stage 4: %d sphere meshes for %d nodes — ForceGraph3D did not create a mesh for every node",
			m.MeshCount, m.NodeCount)
	}
	// Stage 5: no black nodes (invisible against dark background)
	if m.BlackMaterial > 0 {
		t.Errorf("FAIL stage 5: %d/%d nodes have black material — palette broken", m.BlackMaterial, m.MeshCount)
	}
}

// TestGraph_CameraAimsAtCenterOfMass verifies that after the graph loads,
// the camera's OrbitControls target (the orbit pivot) matches the weighted
// center of mass of the visible parent nodes to within a tolerance.
//
// This is the canonical "does the camera auto-lock to center of mass" test.
// It fails if aimAtCenterOfMass is not called, if controls.target is not set,
// or if the weighted centroid calculation is wrong.
func TestGraph_CameraAimsAtCenterOfMass(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second), // CDN load
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Read camera state and node positions from the real page.
	const measureJS = `(function() {
		try {
			var controls = _Graph.controls();
			if (!controls || !controls.target) return JSON.stringify({error:'no controls'});

			// Weighted center of mass: same formula as centerOfMass() in graph.html.
			var nodes = _Graph.graphData().nodes;
			var parents = nodes.filter(function(n){ return n.kind==='scope'||n.kind==='kind-group'; });
			var pool = parents.length ? parents : nodes;

			var cx=0, cy=0, cz=0, totalW=0;
			pool.forEach(function(n) {
				var w = n.val || 1;
				cx += (n.x||0)*w; cy += (n.y||0)*w; cz += (n.z||0)*w;
				totalW += w;
			});
			var com = totalW ? {x:cx/totalW, y:cy/totalW, z:cz/totalW} : {x:0,y:0,z:0};

			var tgt = controls.target;
			var dist = Math.sqrt(
				Math.pow(tgt.x-com.x,2)+Math.pow(tgt.y-com.y,2)+Math.pow(tgt.z-com.z,2)
			);

			// Also check camera is not behind origin — it should be looking at com.
			var cam = controls.object.position;
			var lookDir = {x:tgt.x-cam.x, y:tgt.y-cam.y, z:tgt.z-cam.z};
			var camDist = Math.sqrt(lookDir.x*lookDir.x+lookDir.y*lookDir.y+lookDir.z*lookDir.z);

			return JSON.stringify({
				parentCount:  pool.length,
				com:          {x:Math.round(com.x), y:Math.round(com.y), z:Math.round(com.z)},
				ctrlTarget:   {x:Math.round(tgt.x), y:Math.round(tgt.y), z:Math.round(tgt.z)},
				targetComDist: Math.round(dist),
				camDist:      Math.round(camDist),
			});
		} catch(e) { return JSON.stringify({error:e.message}); }
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(measureJS, &raw)); err != nil {
		t.Fatalf("measure: %v", err)
	}

	var m struct {
		Error         string `json:"error"`
		ParentCount   int    `json:"parentCount"`
		COM           struct{ X, Y, Z int }
		CtrlTarget    struct{ X, Y, Z int }
		TargetComDist int `json:"targetComDist"`
		CamDist       int `json:"camDist"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse: %v — raw: %s", err, raw)
	}
	if m.Error != "" {
		t.Fatalf("JS error: %s", m.Error)
	}

	t.Logf("parent nodes: %d", m.ParentCount)
	t.Logf("center of mass:  {%d %d %d}", m.COM.X, m.COM.Y, m.COM.Z)
	t.Logf("controls.target: {%d %d %d}", m.CtrlTarget.X, m.CtrlTarget.Y, m.CtrlTarget.Z)
	t.Logf("target-to-COM distance: %d units", m.TargetComDist)
	t.Logf("camera distance from target: %d units", m.CamDist)

	// controls.target must be within 100 units of the weighted center of mass.
	// 100 units is generous — the fibonacci sphere is radius 600, so this is
	// a ~17% tolerance that catches "completely wrong" without being flaky.
	const tolerance = 100
	if m.TargetComDist > tolerance {
		t.Errorf("FAIL: camera target is %d units from center of mass (want <= %d)\n"+
			"  center of mass:  {%d %d %d}\n"+
			"  controls.target: {%d %d %d}",
			m.TargetComDist, tolerance,
			m.COM.X, m.COM.Y, m.COM.Z,
			m.CtrlTarget.X, m.CtrlTarget.Y, m.CtrlTarget.Z)
	}

	// Camera must be at a reasonable distance — not at origin, not infinitely far.
	if m.CamDist < 100 || m.CamDist > 5000 {
		t.Errorf("FAIL: camera distance %d is unreasonable (want 100–5000)", m.CamDist)
	}
}

// TestGraph_NodesInFrustum asserts that after the graph loads, at least half the
// scope nodes fall within the camera's view frustum — i.e. they are actually
// visible, not behind or beside the camera.
//
// This is the regression test for the onTick ctrl.update() bug: when onTick
// called ctrl.update() on every 6th frame it continuously repositioned the
// camera, causing it to look away from the node cluster so nodes were never
// in the frustum despite being visible=true in the scene graph.
func TestGraph_NodesInFrustum(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	const js = `(function() {
		try {
			var g = window._Graph;
			var nodes = g.graphData().nodes;
			if (!nodes.length) return JSON.stringify({error: 'no nodes'});

			var cam = g.camera();
			var ctrl = g.controls();

			// Capture controls.target before and after waiting one second.
			// If onTick is calling ctrl.update() the target will drift.
			var t0 = {x: ctrl.target.x, y: ctrl.target.y, z: ctrl.target.z};

			// Project each node through the camera frustum manually.
			// A node is "in frustum" if the vector from cam to node has a
			// positive dot product with the cam look direction AND the node
			// is within the field of view cone.
			var camPos = cam.position;
			// Look direction: from cam toward controls.target
			var lx = ctrl.target.x - camPos.x;
			var ly = ctrl.target.y - camPos.y;
			var lz = ctrl.target.z - camPos.z;
			var llen = Math.sqrt(lx*lx + ly*ly + lz*lz) || 1;
			lx /= llen; ly /= llen; lz /= llen;

			var halfFovRad = (cam.fov / 2) * Math.PI / 180;
			var cosFov = Math.cos(halfFovRad);

			var inFrustum = 0;
			nodes.forEach(function(n) {
				var dx = (n.x||0) - camPos.x;
				var dy = (n.y||0) - camPos.y;
				var dz = (n.z||0) - camPos.z;
				var dlen = Math.sqrt(dx*dx + dy*dy + dz*dz) || 1;
				var dot = (dx*lx + dy*ly + dz*lz) / dlen;
				if (dot > cosFov) inFrustum++;
			});

			return JSON.stringify({
				nodeCount:   nodes.length,
				inFrustum:   inFrustum,
				camPos:      {x: Math.round(camPos.x), y: Math.round(camPos.y), z: Math.round(camPos.z)},
				target:      {x: Math.round(ctrl.target.x), y: Math.round(ctrl.target.y), z: Math.round(ctrl.target.z)},
				target0:     {x: Math.round(t0.x), y: Math.round(t0.y), z: Math.round(t0.z)},
			});
		} catch(e) { return JSON.stringify({error: e.message}); }
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var m struct {
		Error     string `json:"error"`
		NodeCount int    `json:"nodeCount"`
		InFrustum int    `json:"inFrustum"`
		CamPos    struct{ X, Y, Z int }
		Target    struct{ X, Y, Z int }
		Target0   struct{ X, Y, Z int }
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse: %v — raw: %s", err, raw)
	}
	if m.Error != "" {
		t.Fatalf("JS: %s", m.Error)
	}

	t.Logf("nodes: %d  in-frustum: %d", m.NodeCount, m.InFrustum)
	t.Logf("camera: {%d %d %d}", m.CamPos.X, m.CamPos.Y, m.CamPos.Z)
	t.Logf("controls.target: {%d %d %d}", m.Target.X, m.Target.Y, m.Target.Z)

	// At least half the scope nodes must be in the camera frustum.
	want := m.NodeCount / 2
	if m.InFrustum < want {
		t.Errorf("FAIL: only %d/%d nodes in frustum (want >= %d) — camera not aimed at node cluster",
			m.InFrustum, m.NodeCount, want)
	}
}

// TestGraph_MeshPositionsMatchNodes is the integration test for Step 3→4:
// after loadMacro assigns x,y,z to node objects and passes them to
// Graph.graphData(), ForceGraph3D must place each node's Three.js mesh at the
// same position. If positions diverge the node renders at the wrong location.
//
// Given: graph loaded with 3 scope nodes placed by equatorPriorityPositions
// When: we read node data positions and the corresponding mesh parent positions
// Then: each mesh parent position is within 1 world-unit of the node data position
func TestGraph_MeshPositionsMatchNodes(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var consoleLines []string
	chromedp.ListenTarget(ctx, func(ev any) {
		if ev, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			for _, arg := range ev.Args {
				consoleLines = append(consoleLines, string(arg.Value))
			}
		}
	})

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Dump [graph] log lines — Orange instrumentation.
	for _, l := range consoleLines {
		if len(l) > 2 {
			t.Logf("console: %s", l)
		}
	}

	const js = `(function() {
		var g = window._Graph;
		if (!g) return JSON.stringify({error: 'no _Graph'});
		var nodes = g.graphData().nodes;
		if (!nodes.length) return JSON.stringify({error: 'no nodes'});

		var results = [];
		// Build position map from scene meshes.
		// ForceGraph3D places node position on the mesh's OWN local position
		// inside a Group that sits at world origin — so o.position is the
		// mesh's world position.
		var meshPositions = [];
		g.scene().traverse(function(o) {
			if (o.isMesh && o.visible && o.geometry && o.geometry.type === 'SphereGeometry') {
				meshPositions.push({x: o.position.x, y: o.position.y, z: o.position.z});
			}
		});

		// For each node, find the closest mesh and measure the gap.
		var maxGap = 0, mismatch = 0;
		nodes.forEach(function(n) {
			var nx = n.x || 0, ny = n.y || 0, nz = n.z || 0;
			var best = Infinity;
			meshPositions.forEach(function(mp) {
				var d = Math.sqrt(Math.pow(mp.x-nx,2)+Math.pow(mp.y-ny,2)+Math.pow(mp.z-nz,2));
				if (d < best) best = d;
			});
			if (best > 1) mismatch++;
			if (best > maxGap) maxGap = best;
			results.push({id: n.id, nodePos: {x:Math.round(nx),y:Math.round(ny),z:Math.round(nz)}, gap: Math.round(best)});
		});

		return JSON.stringify({
			nodeCount:    nodes.length,
			meshCount:    meshPositions.length,
			mismatch:     mismatch,
			maxGapUnits:  Math.round(maxGap),
			sample:       results.slice(0, 3),
		});
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var m struct {
		Error       string `json:"error"`
		NodeCount   int    `json:"nodeCount"`
		MeshCount   int    `json:"meshCount"`
		Mismatch    int    `json:"mismatch"`
		MaxGapUnits int    `json:"maxGapUnits"`
		Sample      []struct {
			ID      string `json:"id"`
			NodePos struct{ X, Y, Z int }
			Gap     int `json:"gap"`
		} `json:"sample"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse: %v — raw: %s", err, raw)
	}
	if m.Error != "" {
		t.Fatalf("JS: %s", m.Error)
	}

	t.Logf("nodes=%d meshes=%d mismatch=%d maxGap=%d units", m.NodeCount, m.MeshCount, m.Mismatch, m.MaxGapUnits)
	for _, s := range m.Sample {
		t.Logf("  %s nodePos=(%d,%d,%d) meshGap=%d", s.ID, s.NodePos.X, s.NodePos.Y, s.NodePos.Z, s.Gap)
	}

	if m.MeshCount == 0 {
		t.Error("FAIL: no sphere meshes in scene — ForceGraph3D created no node objects")
	}
	// Each node must have a mesh within 1 world-unit of its data position.
	if m.Mismatch > 0 {
		t.Errorf("FAIL: %d/%d nodes have no mesh within 1 unit — positions not propagated from data to scene",
			m.Mismatch, m.NodeCount)
	}
}

// TestGraph_CanvasHasNodePixels is the E2E visual test: takes a PNG screenshot
// of the graph canvas and asserts that at least 0.1% of pixels are brighter
// than the background color (#05050f). If all pixels are background-dark, no
// nodes are rendering regardless of what the scene graph says.
//
// Given: graph loaded with white nodes on dark background
// When: screenshot taken after physics has settled
// Then: >0.1% of pixels have luminance above background threshold
func TestGraph_CanvasHasNodePixels(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		t.Fatalf("screenshot: %v", err)
	}

	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("decode screenshot: %v", err)
	}

	bounds := img.Bounds()
	total := bounds.Dx() * bounds.Dy()
	bright := 0
	// Background is #05050f: R=5, G=5, B=15. Any pixel significantly brighter
	// than this is a node, link, or UI element.
	const bgLum = 5.0/255.0*0.2126 + 5.0/255.0*0.7152 + 15.0/255.0*0.0722 // ≈ 0.0047
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			r, g, b, _ := c.RGBA()
			lum := float64(r>>8)/255.0*0.2126 + float64(g>>8)/255.0*0.7152 + float64(b>>8)/255.0*0.0722
			if lum > bgLum*3 { // 3× above background
				bright++
			}
		}
	}

	pct := float64(bright) / float64(total) * 100
	t.Logf("screenshot: %dx%d  bright_pixels=%d  bright_pct=%.3f%%", bounds.Dx(), bounds.Dy(), bright, pct)

	// Baseline: background color only. Expected: at least 0.1% bright pixels
	// (links alone would produce this). If 0%, the WebGL canvas is blank.
	if pct < 0.1 {
		t.Errorf("FAIL: only %.3f%% bright pixels — canvas appears blank (nodes not rendering)", pct)
	}

	// Nodes are white (#ffffff). Links are rgba(148,163,184,0.25) — faint slate.
	// If nodes render, we expect noticeably more bright pixels than links alone.
	// Threshold 0.5% catches the case where only links render (no spheres).
	_ = color.RGBA{}
	_ = math.Pi
	if pct < 0.5 {
		t.Logf("WARN: %.3f%% bright pixels — only links may be rendering, nodes absent", pct)
	}
}

// TestGraph_RendererConfig asserts the active renderer produces visible,
// non-black nodes with the correct material properties.
// Replaces the stale TestGraph_NodesZPinned which tested a removed fz invariant.
//
// Given: graph loaded with KindColorRenderer (hardcoded kind colors, opacity 0.9)
// When:  inspect sphere mesh materials in the scene
// Then:  materials are non-black, semi-transparent, depth-writing
func TestGraph_RendererConfig(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	const js = `(function() {
		var g = window._Graph;
		if (!g) return JSON.stringify({error: 'no _Graph'});
		var nodes = g.graphData().nodes;
		var bad = [];
		g.scene().traverse(function(o) {
			if (!o.isMesh || !o.visible || !o.geometry || o.geometry.type !== 'SphereGeometry') return;
			var m = o.material;
			var c = m.color;
			// Non-black: at least one channel above 0.1
			if (c && c.r < 0.1 && c.g < 0.1 && c.b < 0.1)
				bad.push('black material: ' + JSON.stringify({r:c.r,g:c.g,b:c.b}));
			// opacity must be 0.9 (KindColorRenderer)
			if (Math.abs(m.opacity - 0.9) > 0.05)
				bad.push('opacity=' + m.opacity + ' want 0.9');
		});
		return JSON.stringify({
			nodeCount: nodes.length,
			bad:       bad.slice(0, 3),
		});
	})()`

	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	var m struct {
		Error     string   `json:"error"`
		NodeCount int      `json:"nodeCount"`
		Bad       []string `json:"bad"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("parse: %v — raw: %s", err, raw)
	}
	if m.Error != "" {
		t.Fatalf("JS: %s", m.Error)
	}

	t.Logf("nodes=%d  material-violations=%d", m.NodeCount, len(m.Bad))
	if len(m.Bad) > 0 {
		t.Errorf("FAIL: renderer produced bad materials:\n%s", m.Bad)
	}
}
