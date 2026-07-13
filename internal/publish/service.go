package publish

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Service is one deployable unit: a version source + image, plus the git repo it
// commits into. For a multi-repo project (cross) Dir == GitDir (one repo per
// service). For a single-repo project (book-reader) several services share one
// GitDir (the repo root) and live in subdirectories.
type Service struct {
	Name    string // display name + folder key, e.g. "user-api" or "book-reader/backend"
	Dir     string // version/scope/build/env dir
	GitDir  string // git repo root used for commit/push/dirty
	Image   string // k8s image name to match; "" when there is none
	TagRepo bool   // publish as a git tag instead of a k8s deploy
}

// IsSingleRepo reports whether the project folder is itself one git repo.
func (c *Config) IsSingleRepo() bool { return isGitRepo(c.ProjectsDir) }

// BaseName is the project folder name, used to derive k8s env dirs and images.
func (c *Config) BaseName() string { return filepath.Base(c.ProjectsDir) }

// k8sEnvDirs overrides the env dir names for projects that don't follow the
// dev-<base>/<base> convention. leia and tts-2 deploy into the shared
// ai-features namespace dir (one dir for both dev and prod). Index 0 is dev,
// 1 is prod.
var k8sEnvDirs = map[string][2]string{
	"leia":  {"ai-features", "ai-features"},
	"tts-2": {"ai-features", "ai-features"},
}

// singleRepoImages overrides the k8s image name for single-repo projects whose
// folder name differs from the deployed image. tts-2 builds the tts-server
// image (ingress host tts.kevyn.com.br) in ai-features.
var singleRepoImages = map[string]string{
	"tts-2": "tts-server",
}

// singleRepoImage resolves the image name for a single-repo service, honouring
// singleRepoImages and falling back to the given default (the folder name).
func singleRepoImage(base, def string) string {
	if o, ok := singleRepoImages[base]; ok {
		return o
	}
	return def
}

// singleRepoSkip lists service folders ("<base>/<folder>") that must never be
// published even though they contain a Dockerfile (e.g. a retired service
// kept around for rollback). Currently empty.
var singleRepoSkip = map[string]bool{}

// DevEnvDir / ProdEnvDir derive the k8s manifest dirs from the project name:
// cross -> dev-cross / cross, book-reader -> dev-book-reader / book-reader.
func (c *Config) DevEnvDir() string {
	if d, ok := k8sEnvDirs[c.BaseName()]; ok {
		return filepath.Join(c.K8SDir, d[0])
	}
	return filepath.Join(c.K8SDir, "dev-"+c.BaseName())
}
func (c *Config) ProdEnvDir() string {
	if d, ok := k8sEnvDirs[c.BaseName()]; ok {
		return filepath.Join(c.K8SDir, d[1])
	}
	return filepath.Join(c.K8SDir, c.BaseName())
}

// DiscoverServices returns the work list for the project, honouring FolderFilter.
func DiscoverServices(cfg *Config) []Service {
	var svcs []Service
	if cfg.IsSingleRepo() {
		svcs = discoverSingle(cfg)
	} else {
		svcs = discoverMulti(cfg)
	}
	if cfg.FolderFilter == "" {
		return svcs
	}
	var out []Service
	for _, s := range svcs {
		if filepath.Base(s.Dir) == cfg.FolderFilter {
			out = append(out, s)
		}
	}
	return out
}

// discoverSingle treats the project folder as one git repo and finds the
// services inside it (depth-1 subdirs that build into an image). If none are
// found the repo itself is the single service.
func discoverSingle(cfg *Config) []Service {
	base := cfg.BaseName()
	root := cfg.ProjectsDir
	var svcs []Service
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !isServiceDir(dir) || singleRepoSkip[base+"/"+e.Name()] {
			continue
		}
		svcs = append(svcs, Service{
			Name:   base + "/" + e.Name(),
			Dir:    dir,
			GitDir: root,
			Image:  base + "/" + e.Name(),
		})
	}
	sort.Slice(svcs, func(i, j int) bool { return svcs[i].Name < svcs[j].Name })
	if len(svcs) == 0 {
		svcs = append(svcs, Service{Name: base, Dir: root, GitDir: root, Image: singleRepoImage(base, base)})
	}
	return svcs
}

// discoverMulti returns one service per git-repo child, ordered by AllRepos with
// any other repos (e.g. tag repos) appended alphabetically.
func discoverMulti(cfg *Config) []Service {
	root := cfg.ProjectsDir
	entries, _ := os.ReadDir(root)
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && isGitRepo(filepath.Join(root, e.Name())) {
			present[e.Name()] = true
		}
	}
	var ordered []string
	for _, r := range AllRepos {
		if present[r] {
			ordered = append(ordered, r)
			delete(present, r)
		}
	}
	var extras []string
	for name := range present {
		extras = append(extras, name)
	}
	sort.Strings(extras)
	ordered = append(ordered, extras...)

	svcs := make([]Service, 0, len(ordered))
	for _, name := range ordered {
		dir := filepath.Join(root, name)
		svcs = append(svcs, Service{
			Name:    name,
			Dir:     dir,
			GitDir:  dir,
			Image:   ImageName(name),
			TagRepo: IsTagRepo(name),
		})
	}
	return svcs
}

// isServiceDir reports whether dir builds into a deployable image: it has a
// Dockerfile and a recognised version source (filters out node_modules, dist…).
func isServiceDir(dir string) bool {
	if !exists(filepath.Join(dir, "Dockerfile")) && !exists(filepath.Join(dir, "Dockerfile.dev")) {
		return false
	}
	return DetectType(dir) != TypeUnknown
}
