package publish

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// patEnvCall matches env-var reads across Go, Python, and JS/TS source files.
var patEnvCall = regexp.MustCompile(
	`(getRequiredEnv|getEnv|os\.Getenv|os\.environ\.get|getenv)\("[A-Z0-9_]+"` +
		`|(process\.env|import\.meta\.env)\.[A-Z0-9_]+`,
)

// ignoredEnvKeys filters out common false positives.
var ignoredEnvKeys = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
	"ENV": true, "VITE": true, "MODE": true, "PROD": true, "DEV": true,
	"URL": true, "HEAD": true, "TODO": true,
}

var envKeyRe = regexp.MustCompile(`[A-Z0-9_]{3,}`)

func extractEnvKeys(hits []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, h := range hits {
		for _, k := range envKeyRe.FindAllString(h, -1) {
			if !ignoredEnvKeys[k] && !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

var sourceExtSuffixes = []string{".go", ".py", ".ts", ".tsx", ".js"}

// CodeEnvsFS returns env keys referenced in the working tree.
func CodeEnvsFS(dir string) []string {
	var hits []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipScanDir(path, dir, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !hasAnySuffix(d.Name(), sourceExtSuffixes) {
			return nil
		}
		data, _ := os.ReadFile(path)
		for _, m := range patEnvCall.FindAllString(string(data), -1) {
			hits = append(hits, m)
		}
		return nil
	})
	return extractEnvKeys(hits)
}

// CodeEnvsHEAD returns env keys referenced at the HEAD commit.
func CodeEnvsHEAD(dir string) []string {
	args := []string{"-C", dir, "grep", "-IE", patEnvCall.String(), "HEAD", "--"}
	for _, ext := range sourceExtSuffixes {
		args = append(args, "*"+ext)
	}
	out, _ := exec.Command("git", args...).Output()
	var hits []string
	for _, line := range strings.Split(string(out), "\n") {
		for _, m := range patEnvCall.FindAllString(line, -1) {
			hits = append(hits, m)
		}
	}
	return extractEnvKeys(hits)
}

// AddedEnvVars returns env keys in the working tree but not at HEAD.
func AddedEnvVars(dir string) []string {
	return setDiff(CodeEnvsFS(dir), CodeEnvsHEAD(dir))
}

var yamlNameRe = regexp.MustCompile(`name: "?([A-Z0-9_]+)`)

// YAMLEnvs returns env key names declared in a k8s manifest.
func YAMLEnvs(yamlPath string) []string {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var keys []string
	for _, m := range yamlNameRe.FindAllSubmatch(data, -1) {
		k := string(m[1])
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// NewEnvVars returns env keys the repo references that are missing from every
// given manifest — a key declared by any of the image's manifests (e.g. the
// WORKER_* vars that live only in workers.yaml) is not "new". Injection
// targets the primary manifest (index 0), so it skips when that one has no
// env: block (e.g. web app deployments).
func NewEnvVars(repoDir string, yamlPaths []string) []string {
	if len(yamlPaths) == 0 || yamlPaths[0] == "" {
		return nil
	}
	data, err := os.ReadFile(yamlPaths[0])
	if err != nil {
		return nil
	}
	if !strings.Contains(string(data), "env:") {
		return nil
	}
	var declared []string
	for _, p := range yamlPaths {
		declared = append(declared, YAMLEnvs(p)...)
	}
	return setDiff(CodeEnvsFS(repoDir), declared)
}

// EnvValue returns a value for key from .env or a default in a call site:
// getEnv("KEY","default") in Go, os.environ.get("KEY","default") /
// os.getenv("KEY","default") / getenv("KEY","default") in Python.
func EnvValue(repoDir, key string) string {
	if f, err := os.Open(filepath.Join(repoDir, ".env")); err == nil {
		defer f.Close()
		prefix := key + "="
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, prefix) {
				return strings.Trim(strings.TrimPrefix(line, prefix), `"`)
			}
		}
	}
	re := regexp.MustCompile(
		`(?:getEnv|os\.environ\.get|os\.getenv|getenv)\(["']` +
			regexp.QuoteMeta(key) + `["'],\s*["']([^"']*)["']`)
	var found string
	_ = filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			if skipScanDir(path, repoDir, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !hasAnySuffix(d.Name(), sourceExtSuffixes) {
			return nil
		}
		data, _ := os.ReadFile(path)
		if m := re.FindSubmatch(data); m != nil {
			found = string(m[1])
		}
		return nil
	})
	return found
}

// skipScanDir reports whether a directory should be skipped when scanning
// source for env-var references or defaults: hidden dirs (.venv, .git…) and
// dependency/cache dirs that would yield false positives.
func skipScanDir(path, root, name string) bool {
	if path != root && strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build", "venv", "__pycache__", "site-packages":
		return true
	}
	return false
}

func setDiff(a, b []string) []string {
	bset := make(map[string]bool, len(b))
	for _, v := range b {
		bset[v] = true
	}
	var out []string
	for _, v := range a {
		if !bset[v] {
			out = append(out, v)
		}
	}
	return out
}

func hasAnySuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}
