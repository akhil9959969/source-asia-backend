package product

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors.
var (
	ErrNotFound     = errors.New("product not found")
	ErrDuplicateSKU = errors.New("SKU already exists")
)

// Store is a concurrency-safe in-memory product store.
//
// Data model (in memory):
//   products  map[id]*Product  – canonical record, keyed by UUID
//   skuIndex  map[sku]id       – uniqueness check + lookup by SKU
//   ordered   []id             – insertion-ordered IDs for stable pagination
//
// List vs Detail query difference:
//   List  (GET /products):      acquires RLock, slices `ordered`, calls
//         toListItem() on each Product. toListItem() only reads len(URLs) and
//         URLs[0] — it never copies the full slices. Fast regardless of depth.
//   Detail(GET /products/:id):  acquires RLock and returns a deep copy of the
//         full Product (all URLs included).
//
// With PostgreSQL + CDN in production:
//   – products table: (id, name, sku, created_at)
//   – product_media table: (id, product_id, kind ENUM('image','video'),
//                           url TEXT, position INT)
//   – List query uses a LEFT JOIN with aggregation (COUNT + MIN for thumbnail)
//     so it never fetches individual URL rows.
//   – Detail query JOINs product_media and returns all rows ordered by position.
//   – CDN URLs are stored as-is; the CDN is responsible for availability.
type Store struct {
	mu       sync.RWMutex
	products map[string]*Product
	skuIndex map[string]string
	ordered  []string
}

// NewStore returns an empty, ready-to-use Store.
func NewStore() *Store {
	return &Store{
		products: make(map[string]*Product),
		skuIndex: make(map[string]string),
	}
}

// newUUID generates a random UUID v4 string using crypto/rand.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Create inserts a new product. Returns ErrDuplicateSKU if SKU is taken.
func (s *Store) Create(name, sku string, imageURLs, videoURLs []string) (*Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.skuIndex[sku]; exists {
		return nil, ErrDuplicateSKU
	}

	// Normalise nil to empty so JSON renders [] not null.
	if imageURLs == nil {
		imageURLs = []string{}
	}
	if videoURLs == nil {
		videoURLs = []string{}
	}

	p := &Product{
		ID:        newUUID(),
		Name:      name,
		SKU:       sku,
		ImageURLs: imageURLs,
		VideoURLs: videoURLs,
		CreatedAt: time.Now().UTC(),
	}

	s.products[p.ID] = p
	s.skuIndex[sku] = p.ID
	s.ordered = append(s.ordered, p.ID)

	return copyProduct(p), nil
}

// List returns a paginated slice of lightweight ListItems and the total count.
// It never touches the full URL slices, keeping the operation O(page_size).
func (s *Store) List(limit, offset int) ([]ListItem, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.ordered)
	if offset >= total {
		return []ListItem{}, total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	slice := s.ordered[offset:end]
	items := make([]ListItem, 0, len(slice))
	for _, id := range slice {
		if p, ok := s.products[id]; ok {
			items = append(items, p.toListItem())
		}
	}
	return items, total
}

// GetByID returns a full deep copy of the product, or ErrNotFound.
func (s *Store) GetByID(id string) (*Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.products[id]
	if !ok {
		return nil, ErrNotFound
	}
	return copyProduct(p), nil
}

// AddMedia appends URLs to an existing product and returns the updated copy.
func (s *Store) AddMedia(id string, imageURLs, videoURLs []string) (*Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.products[id]
	if !ok {
		return nil, ErrNotFound
	}

	p.ImageURLs = append(p.ImageURLs, imageURLs...)
	p.VideoURLs = append(p.VideoURLs, videoURLs...)

	return copyProduct(p), nil
}

// copyProduct returns a deep copy so callers cannot mutate internal store state.
func copyProduct(p *Product) *Product {
	cp := *p
	cp.ImageURLs = append([]string{}, p.ImageURLs...)
	cp.VideoURLs = append([]string{}, p.VideoURLs...)
	return &cp
}
