package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache remembers session summaries so unchanged transcripts are not re-read.
// An entry is valid only while the transcript's size and modification time match.
type Cache struct {
	path string
	mu   sync.Mutex
	data cacheFile
}

type cacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	Session Session   `json:"session"`
}

// cacheFormat is bumped whenever Session's shape changes, so stale entries
// from an older build are ignored rather than misread.
const cacheFormat = 2

// LoadCache reads a cache file. A missing, unreadable, or corrupt file is not an
// error: it simply yields an empty cache, and the scan falls back to reading.
func LoadCache(path string) *Cache {
	c := &Cache{path: path, data: cacheFile{Version: cacheFormat, Entries: map[string]cacheEntry{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var loaded cacheFile
	if json.Unmarshal(b, &loaded) != nil || loaded.Version != cacheFormat || loaded.Entries == nil {
		return c
	}
	c.data.Entries = loaded.Entries
	return c
}

// NewCache is an empty cache that will still be written to path. It is what
// --refresh uses: re-read everything now, but leave the next run a warm cache.
func NewCache(path string) *Cache {
	return &Cache{path: path, data: cacheFile{Version: cacheFormat, Entries: map[string]cacheEntry{}}}
}

func (c *Cache) lookup(path string, size int64, mod time.Time) (Session, bool) {
	if c == nil {
		return Session{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data.Entries[path]
	if !ok || e.Size != size || !e.ModTime.Equal(mod) {
		return Session{}, false
	}
	return e.Session, true
}

func (c *Cache) store(path string, size int64, mod time.Time, s Session) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.Entries[path] = cacheEntry{Size: size, ModTime: mod, Session: s}
}

// Save writes the cache atomically, creating its directory if needed.
func (c *Cache) Save() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c.data)
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Prune drops entries for transcripts that are no longer present, so the cache
// does not grow without bound as sessions are deleted.
func (c *Cache) Prune(live map[string]bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.data.Entries {
		if !live[p] {
			delete(c.data.Entries, p)
		}
	}
}

// DefaultCachePath is where the cache lives between runs.
func DefaultCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude-sessions-cache.json"
	}
	return filepath.Join(home, ".claude", "sessions-cli", "cache.json")
}
