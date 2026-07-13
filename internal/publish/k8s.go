package publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// FindK8sYAMLs searches envDir for every manifest that deploys imageName —
// an image can appear in more than one file (e.g. book-reader's backend image
// runs the backend deployment AND all the role-worker deployments in
// workers.yaml, which must stay on the same version). Only matches the
// image: line to avoid false positives from env URLs. WalkDir is lexical, so
// the first entry is deterministic (the service's own manifest sorts before
// workers.yaml) and is used as the env-injection target.
func FindK8sYAMLs(imageName, envDir string) []string {
	pattern := regexp.MustCompile(`image:.*\/` + regexp.QuoteMeta(imageName) + `:`)
	var found []string
	_ = filepath.WalkDir(envDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		data, _ := os.ReadFile(path)
		if pattern.Match(data) {
			found = append(found, path)
		}
		return nil
	})
	return found
}

// UpdateK8sImage rewrites the image tag in the manifest.
// Returns a human-readable "oldTag -> newTag" string.
func UpdateK8sImage(yamlPath, imageName, newVer string, apply bool) string {
	if newVer == "" || newVer == "0.0.0" {
		return "skip (no version detected in source)"
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return "skip (could not read manifest)"
	}

	lineRe := regexp.MustCompile(`image:.*\/` + regexp.QuoteMeta(imageName) + `:([^\s]+)`)
	m := lineRe.FindSubmatch(data)
	if m == nil {
		return "skip (no image line found)"
	}
	oldTag := string(m[1])

	// preserve -dev suffix
	suffix := ""
	if strings.HasSuffix(oldTag, "-dev") {
		suffix = "-dev"
	}
	newTag := newVer + suffix
	result := fmt.Sprintf("%s -> %s", oldTag, newTag)

	if oldTag == newTag || !apply {
		return result
	}

	updated := lineRe.ReplaceAllFunc(data, func(match []byte) []byte {
		return regexp.MustCompile(regexp.QuoteMeta(string(m[1]))+`$`).
			ReplaceAll(match, []byte(newTag))
	})
	_ = os.WriteFile(yamlPath, updated, 0644)
	return result
}

// InjectEnvVars inserts env vars missing from every manifest of the image
// into the primary manifest's (yamlPaths[0]) env: block.
// Returns the list of added "KEY=value" pairs.
func InjectEnvVars(yamlPaths []string, repoDir string, apply bool) []string {
	missing := NewEnvVars(repoDir, yamlPaths)
	if len(missing) == 0 {
		return nil
	}
	yamlPath := yamlPaths[0]

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil
	}

	// derive indent from the env: line
	envLineRe := regexp.MustCompile(`(?m)^([ \t]*)env:[ \t]*$`)
	m := envLineRe.FindSubmatch(data)
	indent := "        "
	if m != nil {
		indent = string(m[1])
	}

	// list items sit two spaces deeper than the env: key; value: two deeper again
	itemIndent := indent + "  "

	var block strings.Builder
	var added []string
	for _, key := range missing {
		val := EnvValue(repoDir, key)
		added = append(added, fmt.Sprintf("%s=%q", key, val))
		fmt.Fprintf(&block, "%s- name: \"%s\"\n%s  value: \"%s\"\n", itemIndent, key, itemIndent, val)
	}

	if apply && block.Len() > 0 {
		// insert right after the first env: line
		blockStr := block.String()
		updated := envLineRe.ReplaceAllFunc(data, func(match []byte) []byte {
			// match aliases data; appending to it in place would overwrite
			// the bytes that follow the env: line in the file buffer
			out := make([]byte, 0, len(match)+1+len(blockStr))
			out = append(out, match...)
			out = append(out, '\n')
			out = append(out, blockStr...)
			return out
		})
		_ = os.WriteFile(yamlPath, updated, 0644)
	}

	return added
}

// CommitAndPushK8s stages the envDir, commits with msg, and pushes.
func CommitAndPushK8s(k8sDir, envDir, msg string) error {
	cmd := exec.Command("git", "-C", k8sDir, "add", "-A", envDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// check if there's anything to commit
	diff := exec.Command("git", "-C", k8sDir, "diff", "--cached", "--quiet")
	if diff.Run() == nil {
		fmt.Println("nothing to commit, working tree clean")
		return nil
	}
	commit := exec.Command("git", "-C", k8sDir, "commit", "-m", msg)
	commit.Stdout = os.Stdout
	commit.Stderr = os.Stderr
	if err := commit.Run(); err != nil {
		return err
	}
	push := exec.Command("git", "-C", k8sDir, "push")
	push.Stdout = os.Stdout
	push.Stderr = os.Stderr
	return push.Run()
}
