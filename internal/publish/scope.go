package publish

import (
	"fmt"
	"os/exec"
	"strings"
)

// excluded pathspecs for git diff (binary/lock files we never want to diff).
var excludePathspecs = []string{
	":(exclude)*.DS_Store",
	":(exclude)*.png",
	":(exclude)*.jpg",
	":(exclude)*.ico",
	":(exclude)*.lock",
	":(exclude)*.sum",
	":(exclude)*-lock.json",
}

// FileClass classifies a changed path as docs, build, or code.
func FileClass(path string) ChangeScope {
	lower := strings.ToLower(path)
	switch {
	case isDocPath(lower):
		return ScopeDocs
	case isBuildPath(lower):
		return ScopeBuild
	default:
		return ScopeCode
	}
}

func isDocPath(p string) bool {
	for _, kw := range []string{
		"readme", ".md", ".mdx", "docs/", "/docs/",
		"example", "/example", "examples/", "/examples/",
		"sample", "/sample", ".txt",
	} {
		if strings.Contains(p, kw) || strings.HasSuffix(p, kw) {
			return true
		}
	}
	return false
}

func isBuildPath(p string) bool {
	for _, kw := range []string{
		"dockerfile", ".dockerignore", ".gitignore", ".env",
		".lock", "-lock.json", "go.mod", "go.sum",
		"requirements", ".github/", ".ci.yml", ".ci.yaml",
	} {
		if strings.Contains(p, kw) || strings.HasSuffix(p, kw) {
			return true
		}
	}
	return false
}

// ScopeFromFiles classifies a list of changed file paths (code wins over build over docs).
func ScopeFromFiles(files []string) ChangeScope {
	if len(files) == 0 {
		return ScopeCode
	}
	hasCode, hasBuild := false, false
	for _, f := range files {
		switch FileClass(f) {
		case ScopeCode:
			hasCode = true
		case ScopeBuild:
			hasBuild = true
		}
	}
	switch {
	case hasCode:
		return ScopeCode
	case hasBuild:
		return ScopeBuild
	default:
		return ScopeDocs
	}
}

// ComputeChangeScope computes the change scope for a repo directory.
func ComputeChangeScope(dir string) ChangeScope {
	// stage untracked files temporarily so they appear in the diff
	exec.Command("git", "-C", dir, "-c", "core.quotepath=false", "add", "-AN", ".").Run() //nolint:errcheck
	args := append([]string{"-C", dir, "-c", "core.quotepath=false", "diff", "--name-only", "--", "."}, excludePathspecs...)
	out, _ := exec.Command("git", args...).Output()
	exec.Command("git", "-C", dir, "-c", "core.quotepath=false", "reset", "-q", ".").Run() //nolint:errcheck

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return ScopeFromFiles(files)
}

// BuildCtx builds the FACTS + DIFF context string for the LLM.
func BuildCtx(dir string, addedEnvVars []string) (string, error) {
	// stage untracked
	exec.Command("git", "-C", dir, "-c", "core.quotepath=false", "add", "-AN", ".").Run() //nolint:errcheck

	baseArgs := []string{"-C", dir, "-c", "core.quotepath=false"}
	diffArgs := append(append([]string{}, baseArgs...), "diff", "--name-only", "--", ".")
	diffArgs = append(diffArgs, excludePathspecs...)

	filesOut, _ := exec.Command("git", diffArgs...).Output()
	var files []string
	for _, l := range strings.Split(string(filesOut), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}

	filterArgs := func(filter string) []string {
		a := append(append([]string{}, baseArgs...), "diff", "--diff-filter="+filter, "--name-only", "--", ".")
		return append(a, excludePathspecs...)
	}

	newFiles, _ := exec.Command("git", filterArgs("A")...).Output()
	deleted, _ := exec.Command("git", filterArgs("DR")...).Output()
	modified, _ := exec.Command("git", filterArgs("M")...).Output()

	statArgs := append(append([]string{}, baseArgs...), "diff", "--stat", "--", ".")
	statArgs = append(statArgs, excludePathspecs...)
	stat, _ := exec.Command("git", statArgs...).Output()

	diffFullArgs := append(append([]string{}, baseArgs...), "diff", "--", ".")
	diffFullArgs = append(diffFullArgs, excludePathspecs...)
	diffFull, _ := exec.Command("git", diffFullArgs...).Output()

	// truncate diff to 320 lines
	diffLines := strings.Split(string(diffFull), "\n")
	if len(diffLines) > 320 {
		diffLines = diffLines[:320]
	}

	nv := strings.Join(addedEnvVars, " ")
	if nv == "" {
		nv = "none"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== FACTS ===\n")
	fmt.Fprintf(&b, "CHANGE SCOPE: %s\n", ScopeFromFiles(files))
	fmt.Fprintf(&b, "NEW FILES:\n%s\n", strings.TrimSpace(string(newFiles)))
	fmt.Fprintf(&b, "DELETED/RENAMED:\n%s\n", strings.TrimSpace(string(deleted)))
	fmt.Fprintf(&b, "MODIFIED FILES:\n%s\n", strings.TrimSpace(string(modified)))
	fmt.Fprintf(&b, "NEW ENV VARS: %s\n\n", nv)
	fmt.Fprintf(&b, "=== DIFF ===\n")
	fmt.Fprintf(&b, "## DIFFSTAT\n%s\n", strings.TrimSpace(string(stat)))
	fmt.Fprintf(&b, "\n## DIFF (truncated)\n%s\n", strings.Join(diffLines, "\n"))

	// unstage
	exec.Command("git", "-C", dir, "-c", "core.quotepath=false", "reset", "-q", ".").Run() //nolint:errcheck

	return b.String(), nil
}
