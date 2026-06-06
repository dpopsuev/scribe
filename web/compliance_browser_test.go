//go:build browser

package web_test

// Layer 3: browser test — virtualCenter (kite anchor) tracks center of mass.
// Verifies the camera does not drift away from the node cloud over time.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestGraph_VirtualCenterTracksCoM(t *testing.T) {
	srv := startServer(t)
	ctx := newBrowser(t)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/graph"),
		chromedp.Sleep(15*time.Second),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}

	// Measure virtualCenter vs weighted CoM and controls.target alignment.
	const js = `(function() {
		try {
			if (!window._virtualCenter) return JSON.stringify({error:'_virtualCenter not exposed'});
			var vc = window._virtualCenter;

			// Weighted CoM — same formula as centerOfMass() in graph.html.
			var nodes = _Graph.graphData().nodes;
			var parents = nodes.filter(function(n){ return n.kind==='scope'||n.kind==='kind-group'; });
			var pool = parents.length ? parents : nodes;
			if (!pool.length) return JSON.stringify({error:'no nodes'});

			var cx=0, cy=0, cz=0, w=0;
			pool.forEach(function(n){ var wt=n.val||1; cx+=(n.x||0)*wt; cy+=(n.y||0)*wt; cz+=(n.z||0)*wt; w+=wt; });
			var com = {x:cx/w, y:cy/w, z:cz/w};

			// Distance between virtualCenter and CoM.
			var vcComDrift = Math.sqrt(
				Math.pow(vc.x-com.x,2)+Math.pow(vc.y-com.y,2)+Math.pow(vc.z-com.z,2));

			// controls.target must equal virtualCenter exactly (set directly each tick).
			var ctrl = _Graph.controls();
			var tgtVcDist = ctrl && ctrl.target
				? Math.sqrt(Math.pow(ctrl.target.x-vc.x,2)+Math.pow(ctrl.target.y-vc.y,2)+Math.pow(ctrl.target.z-vc.z,2))
				: -1;

			return JSON.stringify({
				vcComDrift:  Math.round(vcComDrift),
				tgtVcDist:   Math.round(tgtVcDist),
				nodeCount:   pool.length,
			});
		} catch(e) { return JSON.stringify({error:e.message}); }
	})()`

	sample := func(label string) (vcComDrift, tgtVcDist int) {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &raw)); err != nil {
			t.Fatalf("%s measure: %v", label, err)
		}
		var m struct {
			Error     string `json:"error"`
			VcComDrift int   `json:"vcComDrift"`
			TgtVcDist  int   `json:"tgtVcDist"`
			NodeCount  int   `json:"nodeCount"`
		}
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("%s parse %q: %v", label, raw, err)
		}
		if m.Error != "" {
			t.Fatalf("%s JS error: %s", label, m.Error)
		}
		t.Logf("%s: nodes=%d  vc-to-CoM=%d  target-to-vc=%d",
			label, m.NodeCount, m.VcComDrift, m.TgtVcDist)
		return m.VcComDrift, m.TgtVcDist
	}

	// t=0: after 15s of ticks, virtualCenter should be tracking CoM.
	drift0, tgtDist0 := sample("t=0")

	// controls.target must be exactly the virtualCenter (set each tick directly).
	if tgtDist0 > 5 {
		t.Errorf("controls.target is %d units from virtualCenter (want ≤ 5) — target not locked to kite", tgtDist0)
	}

	// virtualCenter must have converged toward CoM.
	// Lerp=0.015 × 150 ticks (15s × ~10 ticks/s) → ≈ 90% arrival.
	// The fibonacci sphere has radius 600 — tolerance = 150 (25% of radius).
	if drift0 > 150 {
		t.Errorf("virtualCenter is %d units from CoM (want ≤ 150) — kite not tracking after 15s", drift0)
	}

	// t=5s later: drift must not have grown (camera not running away).
	time.Sleep(5 * time.Second)
	drift1, _ := sample("t=+5s")

	if drift1 > drift0+50 {
		t.Errorf("DRIFT GROWING: %d → %d — camera running away from CoM", drift0, drift1)
	}

	fmt.Printf("virtualCenter kite: stable (drift %d → %d)\n", drift0, drift1)
}
