package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hoijun/fleet/internal/server/pgstore"
)

// maxSyncBodyBytes caps the size of a POST /sync request body, so an
// oversized payload is rejected instead of buffered in full before decoding.
const maxSyncBodyBytes = 1 << 20 // 1 MiB

// Sync serves the authenticated document sync API.
type Sync struct {
	Store pgstore.Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Get handles GET /sync?since=<int64>.
func (s Sync) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserID(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "bad since", http.StatusBadRequest)
			return
		}
		since = n
	}
	docs, cursor, err := s.Store.Pull(r.Context(), userID, since)
	if err != nil {
		http.Error(w, "pull failed", http.StatusInternalServerError)
		return
	}
	if docs == nil {
		docs = []pgstore.Doc{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs, "cursor": cursor})
}

// Post handles POST /sync {docs:[Doc]}.
func (s Sync) Post(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserID(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSyncBodyBytes)
	var req struct {
		Docs []pgstore.Doc `json:"docs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	results, cursor, err := s.Store.Push(r.Context(), userID, req.Docs)
	if err != nil {
		http.Error(w, "push failed", http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []pgstore.PushResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "cursor": cursor})
}
