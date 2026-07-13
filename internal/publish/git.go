package publish

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsDirty returns true if the repo at dir has uncommitted changes (ignoring .DS_Store).
func IsDirty(dir string) (bool, error) {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, ".DS_Store") {
			return true, nil
		}
	}
	return false, nil
}

// ResetDSStore removes .DS_Store from the git index.
func ResetDSStore(dir string) {
	exec.Command("git", "-C", dir, "reset", "-q", "--", "*.DS_Store", ".DS_Store").Run() //nolint:errcheck
}

// AddAll stages all changes in dir.
func AddAll(dir string) error {
	cmd := exec.Command("git", "-C", dir, "add", "-A")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Commit creates a commit with msg in dir.
func Commit(dir, msg string) error {
	cmd := exec.Command("git", "-C", dir, "commit", "-m", msg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Push pushes the current branch of dir.
func Push(dir string) error {
	cmd := exec.Command("git", "-C", dir, "push")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// TagAndPush creates an annotated tag and pushes it.
func TagAndPush(dir, tag, msg string) error {
	if err := gitRun(dir, "tag", "-a", tag, "-m", msg); err != nil {
		return fmt.Errorf("git tag: %w", err)
	}
	if err := gitRun(dir, "push", "origin", tag); err != nil {
		return fmt.Errorf("git push tag: %w", err)
	}
	return nil
}

// GHReleaseCreate creates a GitHub release (best-effort, ignores errors).
func GHReleaseCreate(dir, tag, msg string) {
	cmd := exec.Command("gh", "release", "create", tag, "--title", tag, "--notes", msg)
	cmd.Dir = dir
	cmd.Run() //nolint:errcheck
}

// TagVersion returns the highest vX.Y.Z git tag for a repo, or "0.0.0".
func TagVersion(dir string) string {
	out, err := exec.Command("git", "-C", dir, "tag", "--sort=v:refname").Output()
	if err != nil {
		return "0.0.0"
	}
	var best string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		bare := strings.TrimPrefix(line, "v")
		if isVersionTag(bare) {
			best = bare
		}
	}
	if best == "" {
		return "0.0.0"
	}
	return best
}

func isVersionTag(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// RemoteURL returns the origin URL for a repo.
func RemoteURL(dir string) string {
	out, _ := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	return strings.TrimSpace(string(out))
}

// GitAddCommitPush stages all, commits with msg, and pushes. Skips commit if nothing staged.
func GitAddCommitPush(dir, msg string) error {
	ResetDSStore(dir)
	if err := gitRun(dir, "add", "-A"); err != nil {
		return err
	}
	// check if there's anything to commit
	out, _ := exec.Command("git", "-C", dir, "diff", "--cached", "--quiet").CombinedOutput()
	_ = out
	err := gitRun(dir, "commit", "-m", msg)
	if err != nil {
		// exit 1 from "nothing to commit" is not a real error
		if strings.Contains(err.Error(), "exit status 1") {
			return nil
		}
		return err
	}
	return gitRun(dir, "push")
}

func gitRun(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
