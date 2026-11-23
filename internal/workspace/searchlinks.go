package workspace

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// SearchFollowingDirectoryLinks expands directory links in a local search.
// Index still decides which entries are eligible in each directory. Results
// keep their workspace paths, with ordinary paths taking precedence over
// aliases when both match the search filters.
func SearchFollowingDirectoryLinks(root string, indexed []string, includes, excludes Globs, canceled func() bool) []string {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}
	type directory struct {
		real, rel string
		parent    *directory
	}
	queue := []*directory{{real: realRoot}}
	indexes := map[string][]string{realRoot: indexed}
	links := make(map[string]map[string]bool)
	seen := make(map[string]bool)
	var files []string
	remaining := MaxIndexed
	for i := 0; i < len(queue) && remaining > 0; i++ {
		if canceled() {
			break
		}
		dir := queue[i]
		entries, ok := indexes[dir.real]
		if !ok {
			entries = Index(dir.real)
			indexes[dir.real] = entries
		}
		for _, entry := range entries {
			if canceled() || len(files) == MaxIndexed {
				return files
			}
			if dir.parent != nil {
				if remaining == 0 {
					return files
				}
				remaining--
			}
			rel := entry
			if dir.rel != "" {
				rel = dir.rel + "/" + entry
			}
			if excludes.Match(rel) {
				continue
			}
			real := filepath.Join(dir.real, filepath.FromSlash(entry))
			if searchLink(real, links) {
				if target, err := filepath.EvalSymlinks(real); err == nil {
					if info, err := os.Stat(target); err == nil && info.IsDir() {
						ancestor := dir
						for ancestor != nil && ancestor.real != target {
							ancestor = ancestor.parent
						}
						if ancestor == nil && !Skipped(path.Base(rel)) {
							queue = append(queue, &directory{real: target, rel: rel, parent: dir})
						}
						continue
					}
					real = target
				}
			}
			if !seen[real] && (len(includes) == 0 || includes.Match(rel)) {
				seen[real] = true
				files = append(files, rel)
			}
		}
	}
	return files
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
