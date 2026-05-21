package product

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Validation constants (all documented in README).
const (
	maxURLLength  = 2048 // characters; mirrors browser address-bar limit
	maxURLsPerReq = 20   // max URLs per array per single request
	defaultLimit  = 20   // default GET /products page size
	maxLimit      = 100  // maximum GET /products page size
)

// Handler handles HTTP requests for the product catalog.
type Handler struct {
	store *Store
}

// NewHandler returns a Handler backed by the given Store.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// writeJSON encodes v as JSON and sends it with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errResp is a shorthand for single-field error responses.
func errResp(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Validation ────────────────────────────────────────────────────────────────

// isValidURL returns true for well-formed http:// or https:// URLs within
// the character limit.
func isValidURL(raw string) bool {
	if len(raw) == 0 || len(raw) > maxURLLength {
		return false
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// validateURLSlice validates a slice of URLs.
// Returns a human-readable error string, or "" if all URLs are valid.
func validateURLSlice(urls []string, field string) string {
	if len(urls) > maxURLsPerReq {
		return field + ": exceeds the maximum of 20 URLs per request"
	}
	for _, u := range urls {
		if !isValidURL(u) {
			return field + `: invalid URL "` + u + `" (must be http/https, max 2048 chars)`
		}
	}
	return ""
}

// parsePagination reads limit and offset from query params.
// Writes a 400 and returns ok=false if values are invalid.
func parsePagination(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	q := r.URL.Query()

	limitStr := q.Get("limit")
	if limitStr == "" {
		limitStr = strconv.Itoa(defaultLimit)
	}
	offsetStr := q.Get("offset")
	if offsetStr == "" {
		offsetStr = "0"
	}

	var err error
	limit, err = strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		errResp(w, http.StatusBadRequest, "limit must be a positive integer")
		return 0, 0, false
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset, err = strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		errResp(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, 0, false
	}

	return limit, offset, true
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// Create handles POST /products.
//
// Expected JSON body:
//
//	{
//	  "name":       "required, non-empty string",
//	  "sku":        "required, non-empty, unique string",
//	  "image_urls": ["optional array of http/https URLs"],
//	  "video_urls": ["optional array of http/https URLs"]
//	}
//
// Responses: 201 Created | 400 Bad Request | 409 Conflict (duplicate SKU).
// We chose 409 for duplicate SKU rather than 400 because the request is
// well-formed JSON; the conflict is a business constraint, not a syntax error.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string   `json:"name"`
		SKU       string   `json:"sku"`
		ImageURLs []string `json:"image_urls"`
		VideoURLs []string `json:"video_urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errResp(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(body.Name) == "" {
		errResp(w, http.StatusBadRequest, "name is required and must not be empty")
		return
	}
	if strings.TrimSpace(body.SKU) == "" {
		errResp(w, http.StatusBadRequest, "sku is required and must not be empty")
		return
	}
	if msg := validateURLSlice(body.ImageURLs, "image_urls"); msg != "" {
		errResp(w, http.StatusBadRequest, msg)
		return
	}
	if msg := validateURLSlice(body.VideoURLs, "video_urls"); msg != "" {
		errResp(w, http.StatusBadRequest, msg)
		return
	}

	p, err := h.store.Create(body.Name, body.SKU, body.ImageURLs, body.VideoURLs)
	if err != nil {
		if errors.Is(err, ErrDuplicateSKU) {
			errResp(w, http.StatusConflict, "SKU already exists")
			return
		}
		errResp(w, http.StatusInternalServerError, "failed to create product")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// List handles GET /products.
//
// Query params:
//   limit  (default 20, max 100)
//   offset (default 0)
//
// Returns lightweight list items — image_urls and video_urls are omitted.
// Only image_count, video_count, and thumbnail_url (first image) are included.
// This keeps serialisation cost O(page_size) even with thousands of URLs.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := parsePagination(w, r)
	if !ok {
		return
	}

	items, total := h.store.List(limit, offset)
	writeJSON(w, http.StatusOK, map[string]any{
		"data":   items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetByID handles GET /products/{id}.
// Returns the full product including all image_urls and video_urls.
// 404 if the id is unknown.
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	// Go 1.22 net/http supports r.PathValue for named route segments.
	id := r.PathValue("id")

	p, err := h.store.GetByID(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			errResp(w, http.StatusNotFound, "product not found")
			return
		}
		errResp(w, http.StatusInternalServerError, "failed to get product")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// AddMedia handles POST /products/{id}/media.
// Appends new URLs to an existing product.
// At least one of image_urls or video_urls must be non-empty.
func (h *Handler) AddMedia(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		ImageURLs []string `json:"image_urls"`
		VideoURLs []string `json:"video_urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errResp(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if len(body.ImageURLs) == 0 && len(body.VideoURLs) == 0 {
		errResp(w, http.StatusBadRequest, "at least one of image_urls or video_urls must be provided and non-empty")
		return
	}
	if msg := validateURLSlice(body.ImageURLs, "image_urls"); msg != "" {
		errResp(w, http.StatusBadRequest, msg)
		return
	}
	if msg := validateURLSlice(body.VideoURLs, "video_urls"); msg != "" {
		errResp(w, http.StatusBadRequest, msg)
		return
	}

	p, err := h.store.AddMedia(id, body.ImageURLs, body.VideoURLs)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			errResp(w, http.StatusNotFound, "product not found")
			return
		}
		errResp(w, http.StatusInternalServerError, "failed to add media")
		return
	}
	writeJSON(w, http.StatusOK, p)
}
