package service

import (
	"context"
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opStatus)
}

var opStatus = Op{
	Name: "status", //nolint:goconst // op name, not sort field
	Run: func(ctx context.Context, svc *Service, _ json.RawMessage) (string, error) {
		store := svc.Proto.Store()

		var dbBytes int64
		if sizer, ok := store.(parchment.DBSizer); ok {
			dbBytes, _ = sizer.DBSizeBytes(ctx)
		}

		scopes := svc.HomeScopes
		if len(scopes) == 0 {
			scopes = []string{"(none)"}
		}

		status := statusReport{
			Version:    svc.Version,
			DBBytes:    dbBytes,
			DBMB:       fmt.Sprintf("%.1f", float64(dbBytes)/(1024*1024)),
			Scopes:     scopes,
			EmbedModel: svc.EmbedModel,
			SessionID:  svc.SessionID,
		}

		b, _ := json.Marshal(status)
		return string(b), nil
	},
}

type statusReport struct {
	Version    string   `json:"version"`
	DBBytes    int64    `json:"db_bytes"`
	DBMB       string   `json:"db_mb"`
	Scopes     []string `json:"scopes"`
	EmbedModel string   `json:"embed_model,omitempty"`
	SessionID  string   `json:"session_id"`
}
