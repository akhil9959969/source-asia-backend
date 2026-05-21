# Source Asia — Backend Assignment

A single HTTP service implementing a **rate-limited request API** (Part 1) and a **product catalog with media** (Part 2).

Built with **Go 1.22 standard library only** — no external dependencies.

> **AI usage:** Claude (Anthropic) was used to assist with code structure and README formatting. All logic, design decisions, and trade-offs are my own.

---

## Quick start

```bash
# Clone / unzip the repository, then:
go run main.go
# Server listens on :8080

# Override port:
PORT=9000 go run main.go
```

Requirements: **Go 1.22 or later** (uses the enhanced `net/http` mux with method+path routing).

---

## Part 1 — Rate-limited API

### POST /request

Accepts a request for a user. Returns **201 Created** on success.

**Why 201?** Each accepted request is a new recorded event in the system, which fits REST semantics better than a plain 200.

#### Request body

```json
{
  "user_id": "alice",
  "payload": { "any": "json value" }
}
```

| Field     | Type            | Rules                  |
|-----------|-----------------|------------------------|
| `user_id` | string          | Required, non-empty    |
| `payload` | any JSON value  | Required               |

#### Responses

| Status | Meaning |
|--------|---------|
| 201 Created | Request accepted |
| 400 Bad Request | Missing/empty `user_id`, missing `payload`, or invalid JSON |
| 429 Too Many Requests | Rate limit exceeded |

#### Example

```bash
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice","payload":{"item":"shoe","qty":2}}'
```

```json
{
  "message": "request accepted successfully",
  "status": "accepted",
  "user_id": "alice"
}
```

Rate-limit exceeded:

```bash
# After 5 accepted requests within 60 s:
{"error":"rate limit exceeded","message":"maximum 5 requests per 60-second rolling window per user"}
# HTTP 429
```

---

### GET /stats

Returns per-user statistics.

#### Response schema

```json
{
  "users": {
    "<user_id>": {
      "accepted_in_window": 3,
      "rejected_total": 1
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `accepted_in_window` | Requests accepted in the **current rolling 60-second window** |
| `rejected_total` | **Cumulative** all-time count of rejected requests (never resets) |

#### Example

```bash
curl -s http://localhost:8080/stats
```

```json
{
  "users": {
    "alice": { "accepted_in_window": 3, "rejected_total": 0 },
    "bob":   { "accepted_in_window": 5, "rejected_total": 2 }
  }
}
```

---

### Rate-limiting implementation

**Algorithm:** Sliding (rolling) window using a per-user slice of accepted-request timestamps.

On every `POST /request`:
1. Lock the user's state.
2. Discard timestamps older than 60 seconds (lazy purge from the front of the slice).
3. If `len(accepted) >= 5` → reject (429) and increment `rejected_total`.
4. Otherwise → append `time.Now()` and return 201.

**Concurrency:** A single `sync.Mutex` on the `Store` serialises all reads and writes. This is correct regardless of how many goroutines call `TryAccept` simultaneously — the lock ensures the count check and the append are atomic.

**Production limitations (single-binary, in-memory):**
- **Restart loses state** — all counters reset. Fix: persist to Redis or a durable store.
- **Single instance only** — two instances do not share state, so the combined limit would be 5 × N per user. Fix: centralised rate-limit store (e.g. Redis `INCR` with TTL, or a token-bucket service).
- **Memory grows** — every unique `user_id` is kept forever. Fix: add a background goroutine that evicts users whose last activity exceeds a TTL.
- **Lock contention** — one global mutex means all users contend. Fix for high throughput: shard the map by `hash(user_id) % N` and use N mutexes.

---

## Part 2 — Product catalog

### POST /products

Creates a new product.

#### Request body

```json
{
  "name": "Widget A",
  "sku": "SKU-001",
  "image_urls": [
    "https://cdn.example.com/products/sku-001/img-1.jpg",
    "https://cdn.example.com/products/sku-001/img-2.jpg"
  ],
  "video_urls": [
    "https://cdn.example.com/products/sku-001/demo.mp4"
  ]
}
```

| Field | Rules |
|-------|-------|
| `name` | Required, non-empty string |
| `sku` | Required, non-empty, **unique** |
| `image_urls` | Optional array of URL strings (see validation) |
| `video_urls` | Optional array of URL strings (see validation) |

#### Responses

| Status | Meaning |
|--------|---------|
| 201 Created | Product created; body contains the full product with server-assigned `id` |
| 400 Bad Request | Validation failure |
| 409 Conflict | SKU already exists |

**Why 409 for duplicate SKU, not 400?** The request body is valid JSON with valid fields. The conflict is a business uniqueness constraint, not a syntax or format error. 409 Conflict is the correct semantic.

#### Example

```bash
curl -s -X POST http://localhost:8080/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Widget A",
    "sku": "SKU-001",
    "image_urls": ["https://cdn.example.com/sku-001/img-1.jpg"],
    "video_urls": ["https://cdn.example.com/sku-001/demo.mp4"]
  }'
```

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Widget A",
  "sku": "SKU-001",
  "image_urls": ["https://cdn.example.com/sku-001/img-1.jpg"],
  "video_urls":  ["https://cdn.example.com/sku-001/demo.mp4"],
  "created_at": "2026-05-21T10:00:00Z"
}
```

---

### GET /products

Returns a **paginated list** of lightweight product summaries. Full `image_urls` and `video_urls` arrays are **never included** in the list response (see performance note below).

#### Query parameters

| Param | Default | Max | Description |
|-------|---------|-----|-------------|
| `limit` | 20 | 100 | Number of items per page |
| `offset` | 0 | — | Number of items to skip |

#### Response schema

```json
{
  "data": [
    {
      "id":            "550e8400-...",
      "name":          "Widget A",
      "sku":           "SKU-001",
      "image_count":   2,
      "video_count":   1,
      "thumbnail_url": "https://cdn.example.com/sku-001/img-1.jpg",
      "created_at":    "2026-05-21T10:00:00Z"
    }
  ],
  "total":  42,
  "limit":  20,
  "offset": 0
}
```

#### Example

```bash
curl -s "http://localhost:8080/products?limit=10&offset=0"
curl -s "http://localhost:8080/products?limit=10&offset=10"   # page 2
```

---

### GET /products/{id}

Returns the **full product** including all `image_urls` and `video_urls`.

| Status | Meaning |
|--------|---------|
| 200 OK | Full product |
| 404 Not Found | Unknown id |

```bash
curl -s http://localhost:8080/products/550e8400-e29b-41d4-a716-446655440000
```

---

### POST /products/{id}/media

Appends new URLs to an existing product.

#### Request body

```json
{
  "image_urls": ["https://cdn.example.com/sku-001/img-3.jpg"],
  "video_urls": ["https://cdn.example.com/sku-001/review.mp4"]
}
```

At least one of `image_urls` or `video_urls` must be provided and non-empty.

| Status | Meaning |
|--------|---------|
| 200 OK | Updated full product |
| 400 Bad Request | No URLs provided, or URL validation failure |
| 404 Not Found | Unknown id |

```bash
curl -s -X POST http://localhost:8080/products/550e8400-e29b-41d4-a716-446655440000/media \
  -H "Content-Type: application/json" \
  -d '{"image_urls":["https://cdn.example.com/extra.jpg"]}'
```

---

### URL validation rules

Applied to every URL in `image_urls` and `video_urls`:

| Rule | Detail |
|------|--------|
| Scheme | Must be `http://` or `https://` |
| Max length | 2 048 characters (matches browser address-bar limit) |
| Max per request | 20 URLs per array per single request |
| Format | Must pass `url.ParseRequestURI` |

---

## Data model and performance

### In-memory layout

```
Store {
  products  map[string]*Product   // id  → full product record
  skuIndex  map[string]string     // sku → id  (uniqueness + lookup)
  ordered   []string              // insertion-ordered list of IDs (pagination)
}

Product {
  ID        string
  Name      string
  SKU       string
  ImageURLs []string   // full slice, only loaded for detail queries
  VideoURLs []string
  CreatedAt time.Time
}
```

### Why GET /products is fast with 1 000 products × 10 images

`GET /products?limit=20` does:
1. `RLock` the store.
2. Slice `ordered[offset : offset+limit]` — touches 20 IDs.
3. For each ID call `toListItem()`, which reads only `len(ImageURLs)` and `ImageURLs[0]`.
4. **Never iterates over or serialises the remaining 9 image URLs per product.**

Cost = O(page_size), not O(total_products × images_per_product).

### What would change with PostgreSQL + CDN in production

**Schema:**

```sql
CREATE TABLE products (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT NOT NULL,
  sku        TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE product_media (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('image','video')),
  url        TEXT NOT NULL,
  position   INT  NOT NULL DEFAULT 0
);

CREATE INDEX ON product_media(product_id, kind, position);
```

**List query** — aggregates in SQL, never fetches individual URL rows:

```sql
SELECT
  p.id, p.name, p.sku, p.created_at,
  COUNT(*) FILTER (WHERE m.kind = 'image') AS image_count,
  COUNT(*) FILTER (WHERE m.kind = 'video') AS video_count,
  (SELECT url FROM product_media
    WHERE product_id = p.id AND kind = 'image'
    ORDER BY position LIMIT 1) AS thumbnail_url
FROM products p
LEFT JOIN product_media m ON m.product_id = p.id
GROUP BY p.id
ORDER BY p.created_at DESC
LIMIT $1 OFFSET $2;
```

**Detail query** — single JOIN, returns all URLs:

```sql
SELECT p.*, m.kind, m.url, m.position
FROM products p
LEFT JOIN product_media m ON m.product_id = p.id
WHERE p.id = $1
ORDER BY m.kind, m.position;
```

**CDN:** URLs are stored as plain strings. The CDN is responsible for serving them. A background job or event hook can validate that URLs are reachable, but that is out of scope for the API layer.

---

## Project structure

```
source-asia-backend/
├── main.go                        # Wires router and starts HTTP server
├── go.mod                         # Module file (stdlib only, no deps)
├── internal/
│   ├── ratelimit/
│   │   ├── store.go               # Sliding-window rate limiter, mutex-safe
│   │   └── handler.go             # POST /request, GET /stats
│   └── product/
│       ├── model.go               # Product and ListItem types
│       ├── store.go               # In-memory store, RWMutex, UUID generation
│       └── handler.go             # All four product endpoints + validation
└── README.md
```

---

## Seed script (optional — useful for load testing)

```bash
# Create 1 000 products to verify GET /products stays fast
for i in $(seq 1 1000); do
  curl -s -X POST http://localhost:8080/products \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"Product $i\",
      \"sku\":  \"SKU-$(printf '%05d' $i)\",
      \"image_urls\": [
        \"https://cdn.example.com/p$i/img-1.jpg\",
        \"https://cdn.example.com/p$i/img-2.jpg\",
        \"https://cdn.example.com/p$i/img-3.jpg\",
        \"https://cdn.example.com/p$i/img-4.jpg\",
        \"https://cdn.example.com/p$i/img-5.jpg\",
        \"https://cdn.example.com/p$i/img-6.jpg\",
        \"https://cdn.example.com/p$i/img-7.jpg\",
        \"https://cdn.example.com/p$i/img-8.jpg\",
        \"https://cdn.example.com/p$i/img-9.jpg\",
        \"https://cdn.example.com/p$i/img-10.jpg\"
      ]
    }" > /dev/null
done

# Should return 20 items instantly (only counts + thumbnail, no full URL arrays)
time curl -s "http://localhost:8080/products?limit=20" | python3 -m json.tool
```

---

## Assumptions and trade-offs

| Decision | Rationale |
|----------|-----------|
| Stdlib only, no Gin/Echo | Zero external dependencies; cleaner module; Go 1.22 stdlib routing is expressive enough for this scope |
| `sync.Mutex` (not `sync.RWMutex`) for rate limiter | Every operation mutates state (purge + append or purge + count); an RWMutex would provide no benefit and adds complexity |
| `sync.RWMutex` for product store | List and detail queries are reads; concurrent reads are safe and common |
| Sliding window (not fixed window) | More accurate; prevents the "double burst at window boundary" problem |
| `rejected_total` is cumulative | Makes it easy to audit total abuse over the service lifetime; per-window rejected count is less useful in practice |
| 409 for duplicate SKU | Semantically correct: the body is valid, the constraint is at the resource level |
| Deep copy on read | Prevents callers from holding a mutable pointer into the store; small cost, correct behaviour |
| No persistence | Assignment states in-memory is sufficient |
| No authentication | Out of scope per assignment |
