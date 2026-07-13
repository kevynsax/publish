package publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Buildable reports whether BuildCheck knows how to build this project type.
func Buildable(t ProjectType) bool {
	return t == TypeGo || t == TypeWeb
}

// BuildCheck builds the project at dir to verify it compiles before committing.
// go vet is used for Go so it works for both library and main packages with no artifacts.
func BuildCheck(dir string, t ProjectType) error {
	switch t {
	case TypeGo:
		return runBuild(dir, "go", "vet", "./...")
	case TypeWeb:
		if !exists(filepath.Join(dir, "tsconfig.json")) {
			return nil
		}
		return runBuild(dir, "npx", "tsc", "--noEmit")
	}
	return nil
}

func runBuild(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
