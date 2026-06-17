package service

import (
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

func TestIsStale(t *testing.T) {
	tests := []struct {
		name string
		art  *parchment.Artifact
		want bool
	}{
		{
			name: "no ref_backend — not stale",
			art:  &parchment.Artifact{Extra: map[string]any{"foo": "bar"}},
			want: false,
		},
		{
			name: "conty — immutable, never stale",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "conty"},
				InsertedAt: time.Now().Add(-365 * 24 * time.Hour),
			},
			want: false,
		},
		{
			name: "emcee — fresh (30min old, TTL 1h)",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "emcee"},
				InsertedAt: time.Now().Add(-30 * time.Minute),
			},
			want: false,
		},
		{
			name: "emcee — stale (2h old, TTL 1h)",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "emcee"},
				InsertedAt: time.Now().Add(-2 * time.Hour),
			},
			want: true,
		},
		{
			name: "locus — fresh (12h old, TTL 24h)",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "locus"},
				InsertedAt: time.Now().Add(-12 * time.Hour),
			},
			want: false,
		},
		{
			name: "locus — stale (48h old, TTL 24h)",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "locus"},
				InsertedAt: time.Now().Add(-48 * time.Hour),
			},
			want: true,
		},
		{
			name: "gundog — stale (10d old, TTL 7d)",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "gundog"},
				InsertedAt: time.Now().Add(-10 * 24 * time.Hour),
			},
			want: true,
		},
		{
			name: "unknown backend — not stale",
			art: &parchment.Artifact{
				Extra:      map[string]any{"ref_backend": "unknown"},
				InsertedAt: time.Now().Add(-999 * time.Hour),
			},
			want: false,
		},
		{
			name: "nil extra — not stale",
			art:  &parchment.Artifact{},
			want: false,
		},
		{
			name: "zero InsertedAt — not stale",
			art: &parchment.Artifact{
				Extra: map[string]any{"ref_backend": "emcee"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStale(tt.art)
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefBackend(t *testing.T) {
	art := &parchment.Artifact{Extra: map[string]any{"ref_backend": "emcee", "ref_id": "jira:AUTH-42"}}
	if got := RefBackend(art); got != "emcee" {
		t.Errorf("RefBackend() = %q, want %q", got, "emcee")
	}
	if got := RefID(art); got != "jira:AUTH-42" {
		t.Errorf("RefID() = %q, want %q", got, "jira:AUTH-42")
	}
}
