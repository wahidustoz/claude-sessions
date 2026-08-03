package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	testMod  = time.Date(2026, 7, 6, 17, 5, 0, 0, time.UTC)
	testSess = Session{ID: "abc", Title: "cached title", Messages: 7}
)

func TestLoadCacheMissingFileGivesEmptyCache(t *testing.T) {
	c := LoadCache(filepath.Join(t.TempDir(), "nope.json"))
	if c == nil {
		t.Fatal("LoadCache(missing) = nil, want an empty usable cache")
	}
	if _, ok := c.lookup("/x.jsonl", 1, testMod); ok {
		t.Error("empty cache reported a hit")
	}
}

func TestLoadCacheCorruptFileGivesEmptyCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	if err := os.WriteFile(p, []byte("{not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadCache(p)
	if c == nil {
		t.Fatal("LoadCache(corrupt) = nil, want an empty usable cache")
	}
	if _, ok := c.lookup("/x.jsonl", 1, testMod); ok {
		t.Error("corrupt cache reported a hit")
	}
}

// NewCache is what --refresh uses: start empty, but still write to the real path
// so the next run benefits from this one.
func TestNewCacheStartsEmptyButSavesToItsPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	seed := LoadCache(p)
	seed.store("/x.jsonl", 100, testMod, testSess)
	if err := seed.Save(); err != nil {
		t.Fatal(err)
	}

	c := NewCache(p)
	if _, ok := c.lookup("/x.jsonl", 100, testMod); ok {
		t.Error("NewCache reported a hit, want an empty cache")
	}
	c.store("/x.jsonl", 100, testMod, testSess)
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := LoadCache(p).lookup("/x.jsonl", 100, testMod); !ok {
		t.Error("NewCache did not save to its path")
	}
}

func TestCacheStoreThenLookupHits(t *testing.T) {
	c := LoadCache(filepath.Join(t.TempDir(), "cache.json"))
	c.store("/x.jsonl", 100, testMod, testSess)
	got, ok := c.lookup("/x.jsonl", 100, testMod)
	if !ok {
		t.Fatal("lookup after store = miss, want hit")
	}
	if got.Title != "cached title" {
		t.Errorf("Title = %q, want %q", got.Title, "cached title")
	}
}

func TestCacheMissesWhenSizeChanged(t *testing.T) {
	c := LoadCache(filepath.Join(t.TempDir(), "cache.json"))
	c.store("/x.jsonl", 100, testMod, testSess)
	if _, ok := c.lookup("/x.jsonl", 101, testMod); ok {
		t.Error("lookup with different size = hit, want miss")
	}
}

func TestCacheMissesWhenModTimeChanged(t *testing.T) {
	c := LoadCache(filepath.Join(t.TempDir(), "cache.json"))
	c.store("/x.jsonl", 100, testMod, testSess)
	if _, ok := c.lookup("/x.jsonl", 100, testMod.Add(time.Second)); ok {
		t.Error("lookup with different mtime = hit, want miss")
	}
}

func TestCacheSurvivesSaveAndReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	c := LoadCache(p)
	c.store("/x.jsonl", 100, testMod, testSess)
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := LoadCache(p).lookup("/x.jsonl", 100, testMod)
	if !ok {
		t.Fatal("lookup after reload = miss, want hit")
	}
	if got.Messages != 7 {
		t.Errorf("Messages = %d, want 7", got.Messages)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "deeper", "cache.json")
	c := LoadCache(p)
	c.store("/x.jsonl", 1, testMod, testSess)
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("cache file not written: %v", err)
	}
}

// The point of the cache is that RootWith does not re-read unchanged transcripts.
// Seeding a recognisably wrong title proves the cached value is what surfaces.
func TestRootWithUsesCachedEntriesInsteadOfRereading(t *testing.T) {
	dir := filepath.Join("testdata", "tree")
	transcript := filepath.Join(dir, "-Users-x-alpha", "aaaa1111-2222-3333-4444-555566667777.jsonl")
	st, err := os.Stat(transcript)
	if err != nil {
		t.Fatal(err)
	}

	c := LoadCache(filepath.Join(t.TempDir(), "cache.json"))
	seeded := Session{ID: "aaaa1111-2222-3333-4444-555566667777", Title: "FROM CACHE", LastTS: testMod}
	c.store(transcript, st.Size(), st.ModTime(), seeded)

	res, err := RootWith(dir, c)
	if err != nil {
		t.Fatalf("RootWith: %v", err)
	}
	var found bool
	for _, s := range res.Sessions {
		if s.ID == seeded.ID {
			found = true
			if s.Title != "FROM CACHE" {
				t.Errorf("Title = %q, want %q (cache was not consulted)", s.Title, "FROM CACHE")
			}
		}
	}
	if !found {
		t.Error("cached session missing from results")
	}
}

func TestRootWithRepopulatesCacheForLaterRuns(t *testing.T) {
	dir := filepath.Join("testdata", "tree")
	c := LoadCache(filepath.Join(t.TempDir(), "cache.json"))
	if _, err := RootWith(dir, c); err != nil {
		t.Fatalf("RootWith: %v", err)
	}
	transcript := filepath.Join(dir, "-Users-x-alpha", "aaaa1111-2222-3333-4444-555566667777.jsonl")
	st, err := os.Stat(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.lookup(transcript, st.Size(), st.ModTime()); !ok {
		t.Error("scanned transcript was not added to the cache")
	}
}

func TestRootWithNilCacheStillWorks(t *testing.T) {
	res, err := RootWith(filepath.Join("testdata", "tree"), nil)
	if err != nil {
		t.Fatalf("RootWith(nil cache): %v", err)
	}
	if len(res.Sessions) != 2 {
		t.Errorf("len(Sessions) = %d, want 2", len(res.Sessions))
	}
}
