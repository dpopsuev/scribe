package keygen

import "testing"

func TestExtractConsonantSkeleton(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"scribe", "SCR"},
		{"locus", "LCS"},
		{"lex", "LEX"},
		{"limes", "LMS"},
		{"origami", "ORG"},
		{"task", "TSK"},
		{"spec", "SPC"},
		{"bug", "BUG"},
		{"sprint", "SPR"},
		{"goal", "GOL"},
		{"go", "GOO"},
		{"a", "AAA"},
		{"ab", "ABB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractConsonantSkeleton(tt.name)
			if got != tt.want {
				t.Errorf("ExtractConsonantSkeleton(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestDeriveKey_NoCollision(t *testing.T) {
	got := DeriveKey("scribe", nil)
	if got != "SCR" {
		t.Errorf("DeriveKey(scribe, nil) = %q, want SCR", got)
	}
}

func TestDeriveKey_Collision(t *testing.T) {
	existing := map[string]bool{"SCR": true}
	got := DeriveKey("scribe", existing)
	if got == "SCR" {
		t.Error("DeriveKey should not return colliding key")
	}
	if got != "SCI" {
		t.Errorf("DeriveKey(scribe, {SCR}) = %q, want SCI (first unused letter from 'scribe')", got)
	}
}

func TestDeriveKey_MultipleCollisions(t *testing.T) {
	existing := map[string]bool{"SCR": true, "SCI": true, "SCB": true}
	got := DeriveKey("scribe", existing)
	if got != "SCE" {
		t.Errorf("DeriveKey(scribe, {SCR,SCI,SCB}) = %q, want SCE", got)
	}
}

func TestDeriveKey_FallbackAZ(t *testing.T) {
	existing := map[string]bool{"SCR": true, "SCI": true, "SCB": true, "SCE": true}
	got := DeriveKey("scribe", existing)
	if len(got) < 3 {
		t.Errorf("DeriveKey returned too short key: %q", got)
	}
	if existing[got] {
		t.Errorf("DeriveKey returned colliding key: %q", got)
	}
}

func TestDeriveKey_Extend4Letters(t *testing.T) {
	existing := make(map[string]bool)
	existing["SCR"] = true
	for c := byte('A'); c <= 'Z'; c++ {
		existing["SC"+string(c)] = true
	}
	got := DeriveKey("scribe", existing)
	if len(got) != 4 {
		t.Errorf("expected 4-letter key, got %q (len=%d)", got, len(got))
	}
	if got != "SCRA" {
		t.Errorf("DeriveKey extended = %q, want SCRA", got)
	}
}

func TestDeriveKey_EmptyName(t *testing.T) {
	got := DeriveKey("", nil)
	if len(got) < 3 {
		t.Errorf("DeriveKey('') = %q, want at least 3 chars", got)
	}
}

func TestDeriveKey_Screen_CollisionWithSCR(t *testing.T) {
	existing := map[string]bool{"SCR": true}
	got := DeriveKey("screen", existing)
	if got == "SCR" {
		t.Error("should not collide")
	}
	if got != "SCE" {
		t.Errorf("DeriveKey(screen, {SCR}) = %q, want SCE", got)
	}
}
