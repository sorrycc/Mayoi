package plugin

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// UserDirs are the folders of the user's plugins: ~/.mayoi, or the data
// folder MAYOI_DATA_DIR names, so that a trial setup and the tests leave
// ~/.mayoi alone.
func UserDirs() Dirs {
	base := os.Getenv("MAYOI_DATA_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".mayoi")
	}
	return Dirs{
		Plugins: filepath.Join(base, "plugins"),
		Scripts: filepath.Join(base, "scripts"),
		Storage: filepath.Join(base, "storage"),
	}
}

// CLIUsage is what mayoi plugin --help prints.
const CLIUsage = `Usage: mayoi plugin <command> [arguments]

Manages the plugins in %s. A running Mayoi loads changes at once.

Commands:
  new <name> [folder]       Write a new plugin into folder, ./<name> by default
  dev [folder]              Load a plugin from its folder, . by default, and again on each save
  install <source>[@ref]    Install from a Git URL, owner/repo of GitHub, or a name of the index
  update [name]...          Update plugins installed from Git, all by default
  remove <name>...          Remove plugins, or the links of mayoi plugin dev
  list                      List the installed plugins
  search [text]             Search the index of plugins
  pack [folder]             Copy the npm packages a plugin imports into vendor/

Examples:
  mayoi plugin new word-count
  mayoi plugin install mayoi-editor/plugin-prettier
  mayoi plugin install https://github.com/me/my-plugin.git@v1.2.0
`

// RunCLI runs mayoi plugin with its arguments, and returns the status to
// exit with.
func RunCLI(args []string, dirs Dirs, stdout, stderr io.Writer) int {
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "mayoi plugin: "+format+"\n", a...)
		return 1
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintf(stdout, CLIUsage, shortHome(dirs.Plugins))
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sub, rest := args[0], args[1:]
	switch sub {
	case "new":
		if len(rest) == 0 {
			return fail("new needs a name")
		}
		dir := rest[0]
		if len(rest) > 1 {
			dir = rest[1]
		}
		if err := Scaffold(dir, rest[0]); err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(stdout, "Wrote %s. Try it with:\n  cd %s && mayoi plugin dev\n", dir, dir)
	case "dev":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		m, err := Link(dirs.Plugins, dir)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(stdout, "Linked %s: Mayoi loads it from %s, and again on each save.\n", m.Name, dir)
		fmt.Fprintf(stdout, "Its log is in Mayoi under Plugins: Show Log. Unlink it with mayoi plugin remove %s.\n", m.Name)
	case "install":
		if len(rest) != 1 {
			return fail("install needs one source")
		}
		url, ref, byName := SplitSpec(rest[0])
		if byName {
			entries, err := FetchIndex(ctx, nil)
			if err != nil {
				return fail("reading the index: %v", err)
			}
			found := false
			for _, e := range entries {
				if e.Name == url {
					url, found = e.Repository, true
					break
				}
			}
			if !found {
				return fail("no plugin %s in the index: mayoi plugin search lists them", url)
			}
		}
		m, err := Install(ctx, dirs.Plugins, url, ref)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(stdout, "Installed %s %s.\n", m.Name, m.Version)
	case "update":
		names := rest
		if len(names) == 0 {
			for name := range ReadRecords(dirs.Plugins) {
				names = append(names, name)
			}
			sort.Strings(names)
		}
		status := 0
		for _, name := range names {
			changed, err := Update(ctx, dirs.Plugins, name)
			switch {
			case err != nil:
				status = fail("%s: %v", name, err)
			case changed:
				fmt.Fprintf(stdout, "Updated %s.\n", name)
			default:
				fmt.Fprintf(stdout, "%s is up to date.\n", name)
			}
		}
		return status
	case "remove", "uninstall":
		if len(rest) == 0 {
			return fail("remove needs a name")
		}
		status := 0
		for _, name := range rest {
			if err := Remove(dirs.Plugins, name); err != nil {
				status = fail("%v", err)
				continue
			}
			fmt.Fprintf(stdout, "Removed %s.\n", name)
		}
		return status
	case "list", "ls":
		recs := ReadRecords(dirs.Plugins)
		entries, _ := os.ReadDir(dirs.Plugins)
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		n := 0
		for _, e := range entries {
			dir := filepath.Join(dirs.Plugins, e.Name())
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				continue
			}
			n++
			m, err := ReadManifest(dir)
			if err != nil {
				fmt.Fprintf(tw, "%s\t\t%v\n", e.Name(), err)
				continue
			}
			from := recs[m.Name].URL
			if fi, _ := os.Lstat(dir); fi != nil && fi.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(dir)
				from = "dev: " + shortHome(target)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Name, m.Version, from)
		}
		tw.Flush()
		if n == 0 {
			fmt.Fprintf(stdout, "No plugins in %s.\n", shortHome(dirs.Plugins))
		}
	case "search":
		entries, err := FetchIndex(ctx, nil)
		if err != nil {
			return fail("reading the index: %v", err)
		}
		q := strings.ToLower(strings.Join(rest, " "))
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, e := range entries {
			hay := strings.ToLower(e.Name + " " + e.Description + " " + strings.Join(e.Tags, " "))
			if q == "" || strings.Contains(hay, q) {
				fmt.Fprintf(tw, "%s\t%s\n", e.Name, e.Description)
			}
		}
		tw.Flush()
	case "pack":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		res, err := Pack(dir)
		if err != nil {
			return fail("%v", err)
		}
		for _, w := range res.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", w)
		}
		fmt.Fprintf(stdout, "Copied %d files into %s and pointed %d of the plugin's files at them.\n", len(res.Files), VendorDir, len(res.Rewrote))
	default:
		return fail("no command %s: mayoi plugin --help lists them", sub)
	}
	return 0
}

func shortHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
