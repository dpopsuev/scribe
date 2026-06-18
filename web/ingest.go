package web

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/dpopsuev/scribe/internal/ingest"
)

// POST /api/v1/ingest — thin HTTP wrapper over ingest.Apply.
func (s *Server) handleAPIIngest(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "unknown"
	}

	var nodes []ingest.NodeRecord
	var edges []ingest.EdgeRecord
	dec := json.NewDecoder(r.Body)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			http.Error(w, "decode error: "+err.Error(), http.StatusBadRequest)
			return
		}
		var typed struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			continue
		}
		switch typed.Type {
		case "node":
			var rec ingest.NodeRecord
			if err := json.Unmarshal(raw, &rec); err == nil {
				nodes = append(nodes, rec)
			}
		case "edge":
			var rec ingest.EdgeRecord
			if err := json.Unmarshal(raw, &rec); err == nil {
				edges = append(edges, rec)
			}
		}
	}

	result, _ := ingest.Apply(r.Context(), s.svc.Proto.Store(), source, nodes, edges)
	w.Header().Set("Content-Type", "application/json")
	if len(result.Errors) > 0 {
		w.WriteHeader(http.StatusMultiStatus)
	}
	_ = json.NewEncoder(w).Encode(result)
}
