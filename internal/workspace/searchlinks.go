package workspace

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DirectoryLinkCandidates identifies entries that might point to another
// directory. It is cached with the local file index.
func DirectoryLinkCandidates(root string, indexed []string) []string {
	cache := make(map[string]map[string]bool)
	var links []string
	for _, rel := range indexed {
		if searchLink(filepath.Join(root, filepath.FromSlash(rel)), cache) {
			links = append(links, rel)
		}
	}
	return links
}

// LinkedSearchPaths keeps the selected search paths and the directory aliases
// that can lead an open document to one of them.
type LinkedSearchPaths struct {
	Files       []string
	root        string
	directories map[string]string // workspace directory alias to its resolved path
	chosen      map[string]string // resolved path to the relative path used for search
}

// ResultPath maps a workspace-relative document to the path in the results.
func (p *LinkedSearchPaths) ResultPath(rel string) (string, bool) {
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if real, ok := p.directories[dir]; ok {
			chosen, found := p.chosen[filepath.Join(real, filepath.FromSlash(strings.TrimPrefix(rel, dir+"/")))]
			return chosen, found
		}
	}
	chosen, ok := p.chosen[filepath.Join(p.root, filepath.FromSlash(rel))]
	return chosen, ok
}

// SearchFollowingDirectoryLinks expands the cached link candidates for a local
// search. Ordinary paths claim files first. Individual file symlinks keep
// their own paths, as in a search that does not follow directory symlinks.
// Nil means none of the candidates led to an eligible directory.
func SearchFollowingDirectoryLinks(root string, indexed, candidates []string, includes, excludes Globs, canceled func() bool) *LinkedSearchPaths {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	type directory struct {
		real, rel string
		parent    *directory
	}
	result := &LinkedSearchPaths{root: realRoot, directories: make(map[string]string)}
	base := &directory{real: realRoot}
	var queue []*directory
	enqueue := func(rel, target string, parent *directory) {
		if Skipped(path.Base(rel)) {
			return
		}
		result.directories[rel] = target
		for ancestor := parent; ancestor != nil; ancestor = ancestor.parent {
			if ancestor.real == target {
				return
			}
		}
		if !excludes.Match(rel) {
			queue = append(queue, &directory{real: target, rel: rel, parent: parent})
		}
	}
	for _, rel := range candidates {
		if canceled() {
			return nil
		}
		if target, ok := searchDirectoryTarget(filepath.Join(realRoot, filepath.FromSlash(rel))); ok {
			enqueue(rel, target, base)
		}
	}
	if len(result.directories) == 0 {
		return nil
	}
	result.chosen = make(map[string]string)
	add := func(rel, real string) {
		if _, seen := result.chosen[real]; !seen && (len(includes) == 0 || includes.Match(rel)) && !excludes.Match(rel) {
			result.chosen[real] = rel
			result.Files = append(result.Files, rel)
		}
	}
	for _, rel := range indexed {
		if canceled() {
			return nil
		}
		if result.directories[rel] == "" {
			add(rel, filepath.Join(realRoot, filepath.FromSlash(rel)))
		}
	}

	indexes := make(map[string][]string)
	links := make(map[string]map[string]bool)
	remaining := MaxIndexed
	for i := 0; i < len(queue) && remaining > 0 && len(result.Files) < MaxIndexed; i++ {
		if canceled() {
			return nil
		}
		dir := queue[i]
		entries, ok := indexes[dir.real]
		if !ok {
			entries = Index(dir.real)
			indexes[dir.real] = entries
		}
		for _, entry := range entries {
			if canceled() {
				return nil
			}
			if remaining == 0 || len(result.Files) == MaxIndexed {
				return result
			}
			remaining--
			rel := dir.rel + "/" + entry
			real := filepath.Join(dir.real, filepath.FromSlash(entry))
			if searchLink(real, links) {
				if target, ok := searchDirectoryTarget(real); ok {
					enqueue(rel, target, dir)
					continue
				}
			}
			add(rel, real)
		}
	}
	return result
}

func searchDirectoryTarget(p string) (string, bool) {
	target, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(target)
	return target, err == nil && info.IsDir()
}

// searchLink picks out links with one directory read per parent rather than
// a stat for every indexed file. On Windows a junction can be irregular.
func searchLink(file string, cache map[string]map[string]bool) bool {
	dir := filepath.Dir(file)
	names, known := cache[dir]
	const modes = fs.ModeSymlink | fs.ModeIrregular
	if !known {
		if entries, err := os.ReadDir(dir); err == nil {
			names = make(map[string]bool)
			for _, e := range entries {
				if e.Type()&modes != 0 {
					names[e.Name()] = true
				}
			}
		}
		cache[dir] = names
	}
	if names == nil {
		info, err := os.Lstat(file)
		return err == nil && info.Mode()&modes != 0
	}
	return names[filepath.Base(file)]
}
