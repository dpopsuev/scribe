package web_test

// Color contrast tests — assert WCAG 3:1 minimum between generated colors
// and their respective backgrounds, using the same Oklch algorithm as layout.html.
//
// The algorithm: makeColor(hue, bgHex) inverts lightness based on background
// luminance (threshold 0.18). These tests verify that inversion is correct
// for both the dark-blue-night graph canvas and a light page background.

import (
	"math"
	"testing"
)

// oklchToLinearRGB converts Oklch to linear sRGB components.
// Returns values that may exceed [0,1] — clamp before use.
func oklchToLinearRGB(l, c, hDeg float64) (r, g, b float64) {
	h := hDeg * math.Pi / 180
	a := c * math.Cos(h)
	bv := c * math.Sin(h)

	l_ := l + 0.3963377774*a + 0.2158037573*bv
	m_ := l - 0.1055613458*a - 0.0638541728*bv
	s_ := l - 0.0894841775*a - 1.2914855480*bv

	lc := l_ * l_ * l_
	mc := m_ * m_ * m_
	sc := s_ * s_ * s_

	r = +4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc
	g = -1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc
	b = -0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc
	return
}

// linearLuminance computes WCAG relative luminance from linear sRGB.
func linearLuminance(r, g, b float64) float64 {
	return 0.2126*math.Max(0, r) + 0.7152*math.Max(0, g) + 0.0722*math.Max(0, b)
}

// hexLuminance computes WCAG relative luminance of a CSS hex color.
func hexLuminance(hex string) float64 {
	if len(hex) < 7 {
		return 0
	}
	parse := func(s string) float64 {
		var v int
		for _, c := range s {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v += int(c - '0')
			case c >= 'a' && c <= 'f':
				v += int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v += int(c-'A') + 10
			}
		}
		f := float64(v) / 255.0
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	r := parse(hex[1:3])
	g := parse(hex[3:5])
	b := parse(hex[5:7])
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// contrastRatio computes WCAG contrast between two luminance values.
func contrastRatio(l1, l2 float64) float64 {
	hi, lo := math.Max(l1, l2), math.Min(l1, l2)
	return (hi + 0.05) / (lo + 0.05)
}

// generateNodeColor applies the same algorithm as makeColor(hue, bgHex) in
// layout.html: if bgLuminance < 0.18 → dark bg → use L=0.73; else L=0.48.
func generateNodeColor(hue, bgLuminance float64) (nodeLuminance float64) {
	l := 0.48 // light background
	c := 0.17
	if bgLuminance < 0.18 { // dark background
		l = 0.73
		c = 0.14
	}
	r, g, b := oklchToLinearRGB(l, c, hue)
	return linearLuminance(r, g, b)
}

// kindHues mirrors KIND_HUES in layout.html exactly.
var kindHues = map[string]float64{
	"task": 220, "spec": 270, "bug": 12, "goal": 52,
	"campaign": 32, "note": 148, "concept": 175, "source": 230,
	"decision": 320, "need": 295, "doc": 162, "ref": 188,
	"context": 106, "journal": 76, "scope": 255, "kind-group": 210,
}

// backgrounds to test against.
var testBackgrounds = []struct {
	name     string
	hex      string
	minRatio float64 // WCAG minimum: 3:1 for UI elements
}{
	{"dark-blue-night (graph canvas)", "#05050f", 3.0},
	{"dark charcoal", "#1a1a2e", 3.0},
	{"light white", "#ffffff", 3.0},
	{"light gray (pico page)", "#f4f4f4", 3.0},
}

// TestColorContrast_NodeOnBackground asserts that every kind color generated
// by the algorithm passes WCAG 3:1 on both dark and light backgrounds.
func TestColorContrast_NodeOnBackground(t *testing.T) {
	for _, bg := range testBackgrounds {
		bgLum := hexLuminance(bg.hex)
		t.Run(bg.name, func(t *testing.T) {
			for kind, hue := range kindHues {
				nodeLum := generateNodeColor(hue, bgLum)
				ratio := contrastRatio(nodeLum, bgLum)
				if ratio < bg.minRatio {
					t.Errorf("kind=%s hue=%.0f bg=%s: contrast=%.2f:1 < %.1f:1 (WCAG fail)",
						kind, hue, bg.hex, ratio, bg.minRatio)
				}
			}
		})
	}
}

// TestColorContrast_ThresholdInverts verifies the algorithm switches correctly
// at the luminance threshold (0.18): dark backgrounds get L=0.73, light get L=0.48.
func TestColorContrast_ThresholdInverts(t *testing.T) {
	const hue = 220.0 // task blue

	darkBgLum := hexLuminance("#05050f")  // ≈ 0.002 — dark-blue-night
	lightBgLum := hexLuminance("#ffffff") // 1.0 — white

	darkNodeLum := generateNodeColor(hue, darkBgLum)
	lightNodeLum := generateNodeColor(hue, lightBgLum)

	// Dark background must produce brighter nodes (higher luminance).
	if darkNodeLum <= lightNodeLum {
		t.Errorf("algorithm did not invert: dark-bg node lum=%.3f should be > light-bg node lum=%.3f",
			darkNodeLum, lightNodeLum)
	}

	// Both must pass 3:1 on their respective backgrounds.
	if r := contrastRatio(darkNodeLum, darkBgLum); r < 3.0 {
		t.Errorf("dark-bg contrast %.2f:1 < 3:1", r)
	}
	if r := contrastRatio(lightNodeLum, lightBgLum); r < 3.0 {
		t.Errorf("light-bg contrast %.2f:1 < 3:1", r)
	}

	t.Logf("dark bg  (#05050f): node lum=%.3f contrast=%.1f:1", darkNodeLum, contrastRatio(darkNodeLum, darkBgLum))
	t.Logf("light bg (#ffffff): node lum=%.3f contrast=%.1f:1", lightNodeLum, contrastRatio(lightNodeLum, lightBgLum))
}

// TestColorContrast_GraphBgIsAlwaysDark verifies the graph canvas color
// (#05050f) falls below the threshold so the algorithm always produces
// bright nodes for it — regardless of page theme.
func TestColorContrast_GraphBgIsAlwaysDark(t *testing.T) {
	const GRAPH_BG = "#05050f"
	const THRESHOLD = 0.18

	lum := hexLuminance(GRAPH_BG)
	if lum >= THRESHOLD {
		t.Errorf("graph bg %s luminance=%.4f is above threshold %.2f — would generate dim node colors",
			GRAPH_BG, lum, THRESHOLD)
	}
	t.Logf("graph bg %s luminance=%.4f (below threshold %.2f → bright nodes)", GRAPH_BG, lum, THRESHOLD)
}

// TestColorContrast_PerceptualDistance verifies that no two kind colors
// in the same background context are too similar to distinguish.
// Minimum Euclidean distance in linear RGB (approximate perceptual proxy).
func TestColorContrast_PerceptualDistance(t *testing.T) {
	const MIN_DIST = 0.08   // empirically: below this they look the same
	const darkBgLum = 0.002 // #05050f

	type colorEntry struct {
		kind    string
		r, g, b float64
	}
	colors := make([]colorEntry, 0, len(kindHues))
	for kind, hue := range kindHues {
		l, c := 0.73, 0.14 // dark-bg mode
		r, g, b := oklchToLinearRGB(l, c, hue)
		colors = append(colors, colorEntry{kind,
			math.Max(0, r), math.Max(0, g), math.Max(0, b)})
	}
	_ = darkBgLum

	for i := 0; i < len(colors); i++ {
		for j := i + 1; j < len(colors); j++ {
			a, b := colors[i], colors[j]
			dist := math.Sqrt(
				((a.r - b.r) * (a.r - b.r)) +
					((a.g - b.g) * (a.g - b.g)) +
					((a.b - b.b) * (a.b - b.b)),
			)
			if dist < MIN_DIST {
				t.Errorf("kinds %s and %s are too similar: dist=%.3f < %.2f",
					a.kind, b.kind, dist, MIN_DIST)
			}
		}
	}
}
