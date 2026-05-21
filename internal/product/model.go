// Package product implements the product-catalog API.
package product

import "time"

// Product is the complete record stored in memory.
// It contains the full image and video URL slices.
type Product struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SKU       string    `json:"sku"`
	ImageURLs []string  `json:"image_urls"`
	VideoURLs []string  `json:"video_urls"`
	CreatedAt time.Time `json:"created_at"`
}

// ListItem is the lightweight shape returned by GET /products.
//
// With 1 000 products × 10 images each, returning the full Product slice would
// force serialising 10 000 URL strings even when the client only needs a grid.
// ListItem holds only the counts and the first image (thumbnail) so the list
// query is O(page_size) regardless of how many media URLs each product has.
type ListItem struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SKU          string    `json:"sku"`
	ImageCount   int       `json:"image_count"`
	VideoCount   int       `json:"video_count"`
	ThumbnailURL string    `json:"thumbnail_url,omitempty"` // first image URL if present
	CreatedAt    time.Time `json:"created_at"`
}

// toListItem builds a ListItem without copying any URL slices.
func (p *Product) toListItem() ListItem {
	item := ListItem{
		ID:         p.ID,
		Name:       p.Name,
		SKU:        p.SKU,
		ImageCount: len(p.ImageURLs),
		VideoCount: len(p.VideoURLs),
		CreatedAt:  p.CreatedAt,
	}
	if len(p.ImageURLs) > 0 {
		item.ThumbnailURL = p.ImageURLs[0]
	}
	return item
}
