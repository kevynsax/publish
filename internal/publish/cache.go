package publish

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CacheSource indicates where a decision came from.
type CacheSource string

const (
	CacheHit  CacheSource = "HIT"
	CacheMiss CacheSource = "MISS"
	CacheDown CacheSource = "DOWN"
)

type CachedDecision struct {
	Source CacheSource
	Decision
}

// cacheKey hashes modelID + prompt + context.
func cacheKey(modelID, prompt, ctx string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s", modelID, prompt, ctx)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// cacheFile builds the cache path, flattening any "/" in the service name
// (e.g. "book-reader/backend") so it stays a single file under cacheDir.
func cacheFile(cacheDir, repoName string) string {
	return filepath.Join(cacheDir, strings.ReplaceAll(repoName, "/", "-")+".cache")
}

// ReadCache returns a cached decision if the cache file exists and the key matches.
func ReadCache(cacheDir, repoName, key string) (Decision, bool) {
	cf := cacheFile(cacheDir, repoName)
	data, err := os.ReadFile(cf)
	if err != nil {
		return Decision{}, false
	}
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != key {
		return Decision{}, false
	}
	return parseDecisionLine(strings.TrimSpace(lines[1])), true
}

// WriteCache persists a decision to disk.
func WriteCache(cacheDir, repoName, key string, d Decision) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	cf := cacheFile(cacheDir, repoName)
	line := fmt.Sprintf("%s|%s|%s", d.Bump, d.Message, d.Reason)
	return os.WriteFile(cf, []byte(key+"\n"+line+"\n"), 0644)
}

// parseDecisionLine decodes "bump|message|reason".
func parseDecisionLine(line string) Decision {
	parts := strings.SplitN(line, "|", 3)
	d := Decision{Bump: BumpPatch, Message: "Update", Reason: ""}
	if len(parts) >= 1 {
		d.Bump = BumpLevel(parts[0])
	}
	if len(parts) >= 2 {
		d.Message = parts[1]
	}
	if len(parts) >= 3 {
		d.Reason = parts[2]
	}
	return d
}

// CacheKeyFor computes the cache key for a repo decision.
func CacheKeyFor(modelID, prompt, ctx string) string {
	return cacheKey(modelID, prompt, ctx)
}
