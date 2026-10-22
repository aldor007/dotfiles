package gitlab

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DiskCache is a tiny TTL cache for read-only GitLab listings (MR lists,
// current user). Entries live under <root>/cache/<sha256(key)>.json with a
// wrapper recording the expiry. Never used for writes or freshness checks.
type DiskCache struct {
	Dir string
	TTL time.Duration
}

// CacheTTLFromEnv reads MR_REVIEW_CACHE_TTL (default 90s, "0" disables).
func CacheTTLFromEnv() time.Duration {
	if s := os.Getenv("MR_REVIEW_CACHE_TTL"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return d // 0 = disabled
		}
	}
	return 90 * time.Second
}

// NewDiskCache builds a cache rooted in the review state dir. Disabled
// (nil) when: TTL is zero, MR_REVIEW_GLAB points at a stub (test mode), or
// the dir cannot be created.
func NewDiskCache() *DiskCache {
	if os.Getenv("MR_REVIEW_GLAB") != "" {
		return nil // stub/test mode: never cache fake responses
	}
	ttl := CacheTTLFromEnv()
	if ttl <= 0 {
		return nil
	}
	dir := filepath.Join(cacheRoot(), "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	return &DiskCache{Dir: dir, TTL: ttl}
}

func cacheRoot() string {
	if h := os.Getenv("MR_REVIEW_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local/share/mr-review")
}

type cacheEntry struct {
	ExpiresAt int64  `json:"expires_at"`
	Body      string `json:"body"` // base64 of raw response
}

// Get returns the cached body when present and unexpired.
func (c *DiskCache) Get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if unmarshalCacheEntry(b, &e) != nil {
		_ = os.Remove(c.path(key)) // corrupt: drop
		return nil, false
	}
	if time.Now().Unix() >= e.ExpiresAt {
		_ = os.Remove(c.path(key))
		return nil, false
	}
	body, err := decodeBase64(e.Body)
	if err != nil {
		_ = os.Remove(c.path(key))
		return nil, false
	}
	return body, true
}

// Put stores a body with the cache TTL (best effort — never errors out).
func (c *DiskCache) Put(key string, body []byte) {
	if c == nil {
		return
	}
	e := cacheEntry{
		ExpiresAt: time.Now().Add(c.TTL).Unix(),
		Body:      encodeBase64(body),
	}
	if b, err := marshalCacheEntry(e); err == nil {
		_ = os.WriteFile(c.path(key), b, 0o600)
	}
}

func (c *DiskCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(sum[:]) + ".json"
	name = strings.ReplaceAll(name, "/", "_")
	return filepath.Join(c.Dir, name)
}

func unmarshalCacheEntry(b []byte, e *cacheEntry) error {
	return json.Unmarshal(b, e)
}

func marshalCacheEntry(e cacheEntry) ([]byte, error) {
	return json.Marshal(e)
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
