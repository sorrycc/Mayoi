package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/MoeForge/moejs"
)

// VendorDir is where pack copies the packages a plugin imports.
const VendorDir = "vendor"

// PackResult is what pack did.
type PackResult struct {
	Files    []string // the files of packages copied, relative to the plugin
	Rewrote  []string // the plugin's files whose imports now name vendor/
	Warnings []string
}

// Pack copies the npm packages that the plugin in dir imports, from its
// node_modules, into vendor/, with what they import in turn, and points
// the imports at the copies: Mayoi loads no package at run time. Only
// packages of ES modules work; one of CommonJS is a warning.
func Pack(dir string) (*PackResult, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Main == "" {
		return nil, errors.New("the plugin has no main to pack")
	}
	p := &packer{dir: dir, res: &PackResult{}, done: map[string]string{}}
	if err := os.RemoveAll(filepath.Join(dir, VendorDir)); err != nil {
		return nil, err
	}
	if err := p.file(filepath.ToSlash(path.Clean(m.Main)), true); err != nil {
		return nil, err
	}
	return p.res, nil
}

type packer struct {
	dir  string
	res  *PackResult
	done map[string]string // a file of the plugin's or a vendor copy, by path from dir
}

var specRe = regexp.MustCompile(`((?:\bfrom|\bimport)\s*\(?\s*)(["'])([^"'\n]+)(["'])`)

// file packs the imports of a file of the plugin, relative to dir; own is
// set for the plugin's own files, which may be rewritten in place.
func (p *packer) file(rel string, own bool) error {
	if _, ok := p.done[rel]; ok {
		return nil
	}
	p.done[rel] = rel
	full := filepath.Join(p.dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	src := string(data)
	var mod *moejs.Module
	if ext := path.Ext(rel); ext == ".ts" || ext == ".mts" || ext == ".tsx" {
		mod, err = moejs.CompileTS(rel, src)
	} else {
		mod, err = moejs.Compile(rel, src)
	}
	if err != nil {
		return fmt.Errorf("%s: %v", rel, err)
	}
	if !own && (strings.Contains(src, "module.exports") || strings.Contains(src, "require(")) {
		p.res.Warnings = append(p.res.Warnings, rel+" looks like CommonJS, which Mayoi cannot run: use the package's ES module build")
	}
	rewrites := map[string]string{}
	for _, spec := range mod.Requests() {
		switch {
		case spec == "mayoi":
		case strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../"):
			target, err := p.existing(path.Join(path.Dir(rel), spec))
			if err != nil {
				return fmt.Errorf("%s imports %q: %v", rel, spec, err)
			}
			if err := p.file(target, own && !strings.HasPrefix(target, VendorDir+"/")); err != nil {
				return err
			}
		case strings.HasPrefix(spec, "node:"):
			return fmt.Errorf("%s imports %q: Mayoi has no Node modules", rel, spec)
		default:
			target, err := p.vendor(rel, spec)
			if err != nil {
				return err
			}
			to, _ := filepath.Rel(filepath.Dir(filepath.FromSlash(rel)), filepath.FromSlash(target))
			to = filepath.ToSlash(to)
			if !strings.HasPrefix(to, ".") {
				to = "./" + to
			}
			rewrites[spec] = to
		}
	}
	if len(rewrites) == 0 {
		return nil
	}
	out := specRe.ReplaceAllStringFunc(src, func(s string) string {
		g := specRe.FindStringSubmatch(s)
		if to, ok := rewrites[g[3]]; ok && g[2] == g[4] {
			return g[1] + g[2] + to + g[4]
		}
		return s
	})
	if err := os.WriteFile(full, []byte(out), 0o644); err != nil {
		return err
	}
	if own {
		p.res.Rewrote = append(p.res.Rewrote, rel)
	}
	return nil
}

// existing finds the file a relative import names, with or without its
// extension.
func (p *packer) existing(rel string) (string, error) {
	for _, cand := range []string{rel, rel + ".ts", rel + ".js", rel + ".mjs", rel + "/index.ts", rel + "/index.js", rel + "/index.mjs"} {
		if st, err := os.Stat(filepath.Join(p.dir, filepath.FromSlash(cand))); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", errors.New("no such file")
}

// vendor copies the file a package import names into vendor/, from the
// node_modules nearest the importing file, and returns the copy's path.
func (p *packer) vendor(from, spec string) (string, error) {
	pkg, sub := splitPackage(spec)
	var root string
	// node_modules beside the importing file, then up to the plugin.
	src := from
	if strings.HasPrefix(from, VendorDir+"/") {
		src = strings.TrimPrefix(from, VendorDir+"/")
	}
	for d := path.Dir(src); ; d = path.Dir(d) {
		cand := filepath.Join(p.dir, filepath.FromSlash(d), "node_modules", filepath.FromSlash(pkg))
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			root = cand
			break
		}
		if d == "." || d == "/" {
			break
		}
	}
	if root == "" {
		return "", fmt.Errorf("%s imports %q, which is not in node_modules: npm install it first", from, spec)
	}
	entry, err := packageEntry(root, sub)
	if err != nil {
		return "", fmt.Errorf("%s imports %q: %v", from, spec, err)
	}
	rel, _ := filepath.Rel(p.dir, entry)
	rel = filepath.ToSlash(rel)
	// node_modules/a/node_modules/b/x.js is vendor/a/node_modules/b/x.js.
	rel = strings.Replace(rel, "node_modules/", "", 1)
	rel = strings.TrimPrefix(rel, "../")
	dest := VendorDir + "/" + rel
	if _, ok := p.done[dest]; ok {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(p.dir, filepath.FromSlash(dest))), 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(entry)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(p.dir, filepath.FromSlash(dest)), data, 0o644); err != nil {
		return "", err
	}
	p.res.Files = append(p.res.Files, dest)
	// The copy imports what the package's file did: relative files of the
	// package are copied as they are found, packages vendored in turn.
	return dest, p.vendorFile(entry, dest)
}

// vendorFile packs the imports of a vendored copy, reading the original
// beside its package's files.
func (p *packer) vendorFile(orig, dest string) error {
	if _, ok := p.done[dest]; ok {
		return nil
	}
	p.done[dest] = dest
	data, err := os.ReadFile(orig)
	if err != nil {
		return err
	}
	src := string(data)
	mod, err := moejs.Compile(dest, src)
	if err != nil {
		return fmt.Errorf("%s: %v", dest, err)
	}
	if strings.Contains(src, "module.exports") || strings.Contains(src, "require(") {
		p.res.Warnings = append(p.res.Warnings, dest+" looks like CommonJS, which Mayoi cannot run: use the package's ES module build")
	}
	rewrites := map[string]string{}
	for _, spec := range mod.Requests() {
		if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
			o := filepath.Join(filepath.Dir(orig), filepath.FromSlash(spec))
			d := path.Join(path.Dir(dest), spec)
			found := ""
			for _, ext := range []string{"", ".js", ".mjs", "/index.js", "/index.mjs"} {
				if st, err := os.Stat(o + filepath.FromSlash(ext)); err == nil && !st.IsDir() {
					found = ext
					break
				}
			}
			if found == "" && filepath.Ext(o) == "" {
				return fmt.Errorf("%s imports %q: no such file", dest, spec)
			}
			if found != "" {
				rewrites[spec] = spec + found
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(p.dir, filepath.FromSlash(d+found))), 0o755); err != nil {
				return err
			}
			b, err := os.ReadFile(o + filepath.FromSlash(found))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(p.dir, filepath.FromSlash(d+found)), b, 0o644); err != nil {
				return err
			}
			if !slices.Contains(p.res.Files, d+found) {
				p.res.Files = append(p.res.Files, d+found)
			}
			if err := p.vendorFile(o+filepath.FromSlash(found), d+found); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(spec, "node:") || spec == "mayoi" {
			if spec != "mayoi" {
				return fmt.Errorf("%s imports %q: Mayoi has no Node modules", dest, spec)
			}
			continue
		}
		target, err := p.vendorFrom(orig, spec)
		if err != nil {
			return err
		}
		to, _ := filepath.Rel(filepath.Dir(filepath.FromSlash(dest)), filepath.FromSlash(target))
		to = filepath.ToSlash(to)
		if !strings.HasPrefix(to, ".") {
			to = "./" + to
		}
		rewrites[spec] = to
	}
	if len(rewrites) > 0 {
		src = specRe.ReplaceAllStringFunc(src, func(s string) string {
			g := specRe.FindStringSubmatch(s)
			if to, ok := rewrites[g[3]]; ok && g[2] == g[4] {
				return g[1] + g[2] + to + g[4]
			}
			return s
		})
		return os.WriteFile(filepath.Join(p.dir, filepath.FromSlash(dest)), []byte(src), 0o644)
	}
	return nil
}

// vendorFrom vendors a package a vendored file imports, found from where
// the original file is.
func (p *packer) vendorFrom(orig, spec string) (string, error) {
	rel, err := filepath.Rel(p.dir, orig)
	if err != nil {
		return "", err
	}
	return p.vendor(filepath.ToSlash(rel), spec)
}

// splitPackage splits "@scope/pkg/sub/x" into "@scope/pkg" and "sub/x".
func splitPackage(spec string) (pkg, sub string) {
	parts := strings.Split(spec, "/")
	n := 1
	if strings.HasPrefix(spec, "@") && len(parts) > 1 {
		n = 2
	}
	if len(parts) <= n {
		return spec, ""
	}
	return strings.Join(parts[:n], "/"), strings.Join(parts[n:], "/")
}

// packageEntry finds the ES module file of a package, or of a path in it:
// its package.json's exports, module, or main.
func packageEntry(root, sub string) (string, error) {
	var pj struct {
		Main    string          `json:"main"`
		Module  string          `json:"module"`
		Exports json.RawMessage `json:"exports"`
	}
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		json.Unmarshal(data, &pj)
	}
	key := "."
	if sub != "" {
		key = "./" + sub
	}
	if len(pj.Exports) > 0 {
		if target := exportTarget(pj.Exports, key); target != "" {
			return filepath.Join(root, filepath.FromSlash(target)), nil
		}
	}
	if sub != "" {
		for _, ext := range []string{"", ".js", ".mjs", "/index.js", "/index.mjs"} {
			cand := filepath.Join(root, filepath.FromSlash(sub+ext))
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
		}
		return "", fmt.Errorf("no file %s in the package", sub)
	}
	for _, f := range []string{pj.Module, pj.Main, "index.mjs", "index.js"} {
		if f == "" {
			continue
		}
		cand := filepath.Join(root, filepath.FromSlash(f))
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
		if st, err := os.Stat(cand + ".js"); err == nil && !st.IsDir() {
			return cand + ".js", nil
		}
	}
	return "", errors.New("the package has no entry file")
}

// exportTarget reads a package.json's exports for key: a string, a map of
// conditions (import, module, default, browser), or a map of subpaths.
func exportTarget(raw json.RawMessage, key string) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if key == "." {
			return s
		}
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	subpaths := false
	for k := range m {
		if strings.HasPrefix(k, ".") {
			subpaths = true
		}
	}
	if subpaths {
		if v, ok := m[key]; ok {
			return exportTarget(v, ".")
		}
		// "./*": "./dist/*.js"
		for k, v := range m {
			if prefix, ok := strings.CutSuffix(k, "*"); ok && strings.HasPrefix(key, prefix) {
				t := exportTarget(v, ".")
				return strings.Replace(t, "*", strings.TrimPrefix(key, prefix), 1)
			}
		}
		return ""
	}
	for _, cond := range []string{"import", "module", "browser", "default"} {
		if v, ok := m[cond]; ok {
			if t := exportTarget(v, "."); t != "" {
				return t
			}
		}
	}
	return ""
}
