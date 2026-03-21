package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/nobuo-miura/porthole/internal/checker"
)

// History keeps the last N check results in memory.
type History struct {
	mu      sync.Mutex
	results []checker.CheckResult
	max     int
}

func NewHistory(max int) *History {
	return &History{max: max, results: make([]checker.CheckResult, 0, max)}
}

func (h *History) Add(r checker.CheckResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.results) >= h.max {
		h.results = h.results[1:]
	}
	h.results = append(h.results, r)
}

func (h *History) All() []checker.CheckResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]checker.CheckResult, len(h.results))
	copy(out, h.results)
	return out
}

// Handler wires all HTTP routes.
type Handler struct {
	history *History
	mux     *http.ServeMux
}

func New(history *History) *Handler {
	h := &Handler{history: history, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /api/check", h.handleCheck)
	h.mux.HandleFunc("POST /api/check/batch", h.handleBatch)
	h.mux.HandleFunc("GET /api/history", h.handleHistory)
	h.mux.HandleFunc("GET /healthz", h.handleHealth)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req checker.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	result, err := checker.Dispatch(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	h.history.Add(result)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Checks []checker.CheckRequest `json:"checks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	type indexed struct {
		i      int
		result checker.CheckResult
	}

	results := make([]checker.CheckResult, len(body.Checks))
	ch := make(chan indexed, len(body.Checks))

	for i, req := range body.Checks {
		go func(i int, req checker.CheckRequest) {
			res, err := checker.Dispatch(r.Context(), req)
			if err != nil {
				res = checker.CheckResult{
					Type:      req.Type,
					Host:      req.Host,
					Port:      req.Port,
					Success:   false,
					Error:     err.Error(),
					CheckedAt: time.Now(),
				}
			}
			ch <- indexed{i, res}
		}(i, req)
	}

	for range body.Checks {
		item := <-ch
		results[item.i] = item.result
		h.history.Add(item.result)
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *Handler) handleHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"checks": h.history.All()})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": "1.0.0"})
}
