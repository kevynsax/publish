package publish

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var versionRe = regexp.MustCompile(`[0-9]+\.[0-9]+(?:\.[0-9]+)?`)

// infoVersionRe matches a yaml `version:` line (e.g. OpenAPI info.version),
// so a root-level swagger.yaml's `openapi: 3.0.3` line is not mistaken for it.
var infoVersionRe = regexp.MustCompile(`(?m)^\s*version:\s*"?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

// DetectType identifies the project type from the directory layout.
func DetectType(dir string) ProjectType {
	if exists(filepath.Join(dir, "package.json")) {
		return TypeWeb
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "cmd", "*", "main.go"))
	for _, m := range matches {
		if fileContains(m, "@version") {
			return TypeGo
		}
	}
	if exists(filepath.Join(dir, "go.mod")) {
		return TypeGo
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*", "version.py")); len(files) > 0 {
		return TypePython
	}
	if exists(filepath.Join(dir, "setup.py")) {
		return TypePython
	}
	if exists(filepath.Join(dir, "version.txt")) {
		return TypeText
	}
	return TypeUnknown
}

// GoMainFile returns the path to the main.go that holds the @version annotation.
func GoMainFile(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "cmd", "*", "main.go"))
	for _, m := range matches {
		if fileContains(m, "@version") {
			return m
		}
	}
	return ""
}

// PyVersionFile returns the first */version.py found.
func PyVersionFile(dir string) string {
	files, _ := filepath.Glob(filepath.Join(dir, "*", "version.py"))
	if len(files) > 0 {
		return files[0]
	}
	return ""
}

// ReadVersion reads the current version from the appropriate source(s), returning the max.
func ReadVersion(dir string, t ProjectType) string {
	var versions []string
	switch t {
	case TypeWeb:
		data, _ := os.ReadFile(filepath.Join(dir, "package.json"))
		if v := firstVersionAfter(string(data), `"version"`); v != "" {
			versions = append(versions, v)
		}
	case TypeGo:
		if mf := GoMainFile(dir); mf != "" {
			if v := firstVersionInLine(mf, "@version"); v != "" {
				versions = append(versions, v)
			}
		}
		if v := infoVersionIn(filepath.Join(dir, "swagger.yaml")); v != "" {
			versions = append(versions, v)
		}
		for _, rel := range []string{"docs/swagger.yaml", "docs/swagger.json", "docs/docs.go"} {
			f := filepath.Join(dir, rel)
			if v := firstVersionIn(f); v != "" {
				versions = append(versions, v)
			}
		}
	case TypePython:
		if pf := PyVersionFile(dir); pf != "" {
			if v := firstVersionInLine(pf, "version"); v != "" {
				versions = append(versions, v)
			}
		}
	case TypeText:
		if v := firstVersionIn(filepath.Join(dir, "version.txt")); v != "" {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return "0.0.0"
	}
	sort.Slice(versions, func(i, j int) bool { return SemverLess(versions[i], versions[j]) })
	return versions[len(versions)-1]
}

// WriteVersion updates the version in all appropriate files for the project type.
func WriteVersion(dir string, t ProjectType, newVer string) error {
	switch t {
	case TypeWeb:
		return replaceInFile(
			filepath.Join(dir, "package.json"),
			`("version"\s*:\s*")[0-9]+\.[0-9]+(?:\.[0-9]+)?(")`,
			"${1}"+newVer+"${2}",
		)
	case TypeGo:
		if mf := GoMainFile(dir); mf != "" {
			if err := replaceInFile(mf, `(@version\s+)[0-9]+\.[0-9]+(?:\.[0-9]+)?`, "${1}"+newVer); err != nil {
				return err
			}
		}
		for _, rel := range []string{"swagger.yaml", "docs/swagger.yaml"} {
			swYAML := filepath.Join(dir, rel)
			if exists(swYAML) {
				replaceInFile(swYAML, `(?m)^(\s*version:\s*"?)[0-9]+\.[0-9]+(?:\.[0-9]+)?("?)`, "${1}"+newVer+"${2}") //nolint:errcheck
			}
		}
		swJSON := filepath.Join(dir, "docs/swagger.json")
		if exists(swJSON) {
			replaceInFile(swJSON, `("version"\s*:\s*")[0-9]+\.[0-9]+(?:\.[0-9]+)?(")`, "${1}"+newVer+"${2}") //nolint:errcheck
		}
		docsGo := filepath.Join(dir, "docs/docs.go")
		if exists(docsGo) {
			replaceInFile(docsGo, `(Version:\s*")[0-9]+\.[0-9]+(?:\.[0-9]+)?(")`, "${1}"+newVer+"${2}") //nolint:errcheck
		}
	case TypePython:
		if pf := PyVersionFile(dir); pf != "" {
			return replaceInFile(pf, `(version\s*=\s*")[0-9]+\.[0-9]+(?:\.[0-9]+)?(")`, "${1}"+newVer+"${2}")
		}
	case TypeText:
		return replaceInFile(filepath.Join(dir, "version.txt"), `[0-9]+\.[0-9]+(?:\.[0-9]+)?`, newVer)
	}
	return nil
}

// replaceInFile applies a regexp substitution across the whole file content.
func replaceInFile(path, pattern, repl string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	return os.WriteFile(path, re.ReplaceAll(data, []byte(repl)), 0644)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileContains(path, substr string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), substr)
}

func infoVersionIn(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := infoVersionRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

func firstVersionIn(path string) string {
	data, _ := os.ReadFile(path)
	return versionRe.FindString(string(data))
}

func firstVersionInLine(path, keyword string) string {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, keyword) {
			if v := versionRe.FindString(line); v != "" {
				return v
			}
		}
	}
	return ""
}

func firstVersionAfter(content, keyword string) string {
	idx := strings.Index(content, keyword)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(keyword):]
	// skip until the colon
	ci := strings.IndexByte(rest, ':')
	if ci < 0 {
		return ""
	}
	return versionRe.FindString(rest[ci:])
}
