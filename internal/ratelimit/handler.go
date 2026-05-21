package ratelimit

import (
	"encoding/json"
	"net/http"
)

// Handler exposes the rate-limit API over HTTP.
type Handler struct {
	store *Store
}

// NewHandler creates a Handler backed by the given Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// requestBody is the expected JSON shape for POST /request.
type requestBody struct {
	UserID  string          `json:"user_id"`
	Payload json.RawMessage `json:"payload"`
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleRequest handles POST /request.
//
// Returns:
//   201 Created           – request accepted.
//   400 Bad Request       – missing/empty user_id, missing payload, or invalid JSON.
//   429 Too Many Requests – rate limit exceeded.
func (h *Handler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON body: " + err.Error(),
		})
		return
	}

	if body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "user_id is required and must not be empty",
		})
		return
	}

	// json.RawMessage is nil when the key was absent entirely.
	if body.Payload == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "payload is required",
		})
		return
	}

	if !h.store.TryAccept(body.UserID) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":   "rate limit exceeded",
			"message": "maximum 5 requests per 60-second rolling window per user",
		})
		return
	}

	// 201 Created: we chose 201 (not 200) because the request creates a new
	// recorded event in our system, which fits REST semantics.
	writeJSON(w, http.StatusCreated, map[string]string{
		"status":  "accepted",
		"user_id": body.UserID,
		"message": "request accepted successfully",
	})
}

// HandleStats handles GET /stats.
//
// Response shape:
//
//	{
//	  "users": {
//	    "<user_id>": {
//	      "accepted_in_window": <int>,
//	      "rejected_total":     <int>
//	    }
//	  }
//	}
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"users": h.store.GetStats(),
	})
}
