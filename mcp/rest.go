package mcp

// REST facade over the service.Op registry — the third adapter alongside
// MCP and CLI. Lock-step is structural: this file contains ZERO per-verb
// logic; every action dispatches through service.Find, so new operations
// appear here automatically when they land in the registry.

import (
	"encoding/json"
	"net/http"

	"github.com/dpopsuev/scribe/service"
)

// restDenylist lists ops NOT exposed over REST. Binary-streaming ops need
// raw bodies, not JSON; keep the list explicit and reviewed.
var restDenylist = map[string]bool{
	"attach": true,
	"detach": true,
}

type restRequest struct {
	Action string `json:"action"`
}

type restResponse struct {
	OK   bool   `json:"ok"`
	Text string `json:"text,omitempty"`
	Data any    `json:"data,omitempty"`
	Err  string `json:"error,omitempty"`
}

// RESTHandler serves POST /api/v1/ops ({action, ...input}) and
// GET /api/v1/ops (verb inventory, for parity checks and discovery).
func RESTHandler(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/ops", func(w http.ResponseWriter, _ *http.Request) {
		names := make([]string, 0, len(service.Registry))
		for _, op := range service.Registry {
			if !restDenylist[op.Name] {
				names = append(names, op.Name)
			}
		}
		writeREST(w, http.StatusOK, map[string]any{"ops": names})
	})

	mux.HandleFunc("POST /api/v1/ops", func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeREST(w, http.StatusBadRequest, restResponse{OK: false, Err: "invalid JSON: " + err.Error()})
			return
		}
		var head restRequest
		_ = json.Unmarshal(raw, &head)
		if head.Action == "" {
			writeREST(w, http.StatusBadRequest, restResponse{OK: false, Err: "missing action"})
			return
		}
		if restDenylist[head.Action] {
			writeREST(w, http.StatusNotImplemented, restResponse{OK: false, Err: "action not exposed over REST: " + head.Action})
			return
		}
		op := service.Find(head.Action)
		if op == nil {
			writeREST(w, http.StatusNotFound, restResponse{OK: false, Err: "unknown action: " + head.Action})
			return
		}
		result, err := op.RunTraced(r.Context(), svc, raw)
		if err != nil {
			writeREST(w, http.StatusOK, restResponse{OK: false, Err: err.Error()})
			return
		}
		writeREST(w, http.StatusOK, restResponse{OK: true, Text: result.Text, Data: result.Data})
	})

	return mux
}

func writeREST(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
