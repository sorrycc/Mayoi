<p align="center">
  <img src="resources/icon.png" width="128" height="128" alt="Mayoi icon">
</p>

<h1 align="center">Mayoi</h1>

<p align="center">迷い — a small, fast code editor written in Go. It draws a native UI on the GPU with
<a href="https://github.com/egoist/mygo">MyGo</a>, with no webview and no Electron, and the packaged app is about 18 MB.</p>

https://github.com/user-attachments/assets/673e7907-2b2c-4ed6-9093-1ba9633cd61c

## Features

- **Editor:** tabs, Chroma syntax highlighting, indent guides, sticky scroll, breadcrumbs and word wrap
- **Smart editing:** auto-indent, auto-closing brackets and quotes, line commands, and per-tab undo
- **Vim mode:** normal, insert and visual modes, with motions, operators, text objects, counts and `.`, registers,
  macros, marks, search, and `:` commands as `:s`, `:g` and `:normal`
- **Columns:** split the editor side by side and drag tabs between columns
- **Explorer:** a live file tree that follows the disk and dims Git-ignored files
- **Quick open:** fuzzy file search, go to line (`:42`), go to symbol (`@`) and a command palette (`>`, ⇧⌘P)
- **Find and replace:** case, whole-word and regex matching, searched in the background in large files
- **Search:** find and replace across the folder's files (⇧⌘F, ⇧⌘H), with files to include and exclude, and a
  preview of each replacement
- **Git:** a source control view to stage, commit, amend, undo, pull and push, every change in one diff, folded
  around the changes, inline or side by side, a graph of the commits with their branches and tags, each with its own
  diff, gutter marks that revert or stage a change, branches and blame
- **Terminal:** Ghostty's emulator (libghostty-vt), in the bottom panel, at a side of the window, or in a tab
- **Remote SSH:** open a folder on another machine with the system's `ssh`, nothing installed there: the Explorer,
  search, Git and terminals run on the host, and its ports forward to this machine
- **Image viewer:** PNG, JPEG, GIF, WebP and BMP
- **Previews:** a live preview of Markdown, SVG and Mermaid beside the source (⇧⌘V), with the Markdown's Mermaid
  blocks drawn as diagrams and its LaTeX math as formulas
- **Windows:** a window per folder or every folder in one window as project tabs, a welcome page with the recent
  folders and hosts, Open Recent (⌃R), and a `mayoi` shell command that opens files at a line, compares them, waits
  for them to close and runs any command
- **Themes:** light and dark, The Matrix with digital rain, Purple Gold, and Claude, Qoder, Grok Bot and Cursor, each in light
  and dark
- **Auto save,** and every window reopened as you left it, with its columns, tabs and carets
- **Settings:** JSON files applied as you save them, with the code's and the terminal's fonts, weight, ligatures and
  line height, and keyboard shortcuts in VS Code's format (see [docs/SETTINGS.md](docs/SETTINGS.md))
- **AI commit messages:** signed in, write the commit message from the changes with Mon's AI gateway
- **Settings sync:** sign in with GitHub or Google, and your settings and keyboard shortcuts follow you between machines

See [docs/FEATURES.md](docs/FEATURES.md) for the details, [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for a tour of the
source and how fast it runs, and [CONTRIBUTING.md](CONTRIBUTING.md) to build, test and release Mayoi.

## Tour

<table>
  <tr>
    <td width="50%"><b>Columns</b>: split the editor and drag tabs between columns<br>
      <a href="docs/screenshots/columns.png"><img alt="Two columns of Go source" src="docs/screenshots/columns.png"></a></td>
    <td width="50%"><b>Vim mode</b>: motions, operators, text objects and visual mode<br>
      <a href="docs/screenshots/vim-visual.png"><img alt="Lines selected in Vim's visual mode" src="docs/screenshots/vim-visual.png"></a></td>
  </tr>
  <tr>
    <td><b>Peek a change</b>: click a gutter mark to see the lines at HEAD, then revert or stage them<br>
      <a href="docs/screenshots/change-peek.png"><img alt="A change peeked from the gutter" src="docs/screenshots/change-peek.png"></a></td>
    <td><b>Side-by-side diff</b>: every change in one tab, with changed words tinted<br>
      <a href="docs/screenshots/scm-diff-split.png"><img alt="Source control with a side-by-side diff" src="docs/screenshots/scm-diff-split.png"></a></td>
  </tr>
  <tr>
    <td><b>Commit graph</b>: branches, tags and merges, and each commit's changes in a tab<br>
      <a href="docs/screenshots/commits.png"><img alt="The commit graph and a commit's diff" src="docs/screenshots/commits.png"></a></td>
    <td><b>Markdown preview</b>: live beside the source<br>
      <a href="docs/screenshots/markdown-preview.png"><img alt="A Markdown file and its preview" src="docs/screenshots/markdown-preview.png"></a></td>
  </tr>
  <tr>
    <td><b>Terminal</b>: Ghostty's emulator in the panel or, here, in a tab<br>
      <a href="docs/screenshots/terminal-tab.png"><img alt="A terminal in a column beside the code" src="docs/screenshots/terminal-tab.png"></a></td>
    <td><b>Command palette</b>: every command by name, with its shortcut<br>
      <a href="docs/screenshots/command-palette.png"><img alt="The command palette" src="docs/screenshots/command-palette.png"></a></td>
  </tr>
  <tr>
    <td><b>Search in files</b>: as you type, with each match in context<br>
      <a href="docs/screenshots/search.png"><img alt="Search across the folder's files" src="docs/screenshots/search.png"></a></td>
    <td><b>Project tabs</b>: every folder in one window, on a rail<br>
      <a href="docs/screenshots/project-tabs.png"><img alt="Projects on a rail at the left of the window" src="docs/screenshots/project-tabs.png"></a></td>
  </tr>
  <tr>
    <td><b>Remote SSH</b>: the Explorer, Git, search and terminals on the host, and its ports here<br>
      <a href="docs/screenshots/remote-window.png"><img alt="A folder open on an SSH host" src="docs/screenshots/remote-window.png"></a></td>
    <td><b>The Matrix</b>: one of the themes, with the whole window in the code's font<br>
      <a href="docs/screenshots/theme-matrix.png"><img alt="The Matrix theme" src="docs/screenshots/theme-matrix.png"></a></td>
  </tr>
</table>

## Installation

### macOS

Requires macOS 13 or later. Install with [Homebrew](https://brew.sh) on Apple Silicon or Intel:

```sh
brew tap --custom-remote sorrycc/mayoi https://github.com/sorrycc/Mayoi.git
brew trust sorrycc/mayoi
brew install --cask mayoi
```

Recent Homebrew refuses to load casks from a tap you haven't trusted, which `brew trust` allows once. On an older
Homebrew without that command, skip that line.

Or download the DMG for your Mac from [Releases](https://github.com/sorrycc/Mayoi/releases/latest) and drag
**Mayoi** to Applications. Both methods install the signed, notarized app.

Open **Mayoi** from Applications, or run `mayoi .` or `mayoi <file>` in a shell.
Homebrew installs and removes the shell command with the app. For a DMG installation, choose
**File → Install 'mayoi' Command** in the app instead.

If you already installed the command through Mayoi, choose **File → Uninstall 'mayoi' Command** before
installing with Homebrew, so only one `mayoi` command is on your `PATH`.

Mayoi checks for updates automatically. To upgrade with Homebrew, run `brew update && brew upgrade --cask mayoi`;
if it refuses to load the cask from an untrusted tap, run `brew trust sorrycc/mayoi` first.

### Linux

Requires GTK 3, such as on Ubuntu 22.04 or later:

```sh
curl -fsSL https://github.com/sorrycc/Mayoi/releases/latest/download/install.sh | sh
```

The installer adds Mayoi to `~/.local`, with the `mayoi` command and an entry in the applications menu.

### Windows

Download the installer for your architecture from [Releases](https://github.com/sorrycc/Mayoi/releases/latest).

### Build from source

Requires Go 1.27 or later; Linux also needs GTK 3. From the repository:

```sh
go run ./cmd/mayoi ~/some/project    # open a folder or a file
go tool mygo build                   # build build/darwin-arm64/Mayoi.app and a .dmg
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and testing.

## Keyboard shortcuts

| Shortcut | Action |
|---|---|
| ⌘P / ⇧⌘O / ⌃G | Quick open / go to symbol / go to line |
| ⌘F / ⌥⌘F / ⌘G | Find / replace / next match |
| ⇧⌘F / ⇧⌘H | Find in files / replace in files |
| ⌘S / ⇧⌘S / ⌥⌘S | Save / save as / save all |
| ⌘O / ⌥⌘O / ⌃R | Open folder / open folder in a new window / open recent |
| ⌘N / ⌘W / ⌘1…9 / ⌃Tab | New file / close tab / go to tab / next tab |
| ⇧⌘] / ⇧⌘[ / ⌃⌘1…9 | Next / previous / go to project, with project tabs |
| ⌘\\ / ⌥⌘→ / ⌥⌘← | Split right / next column / previous column |
| ⌘B / ⌃\` / ⌃⇧\` | Toggle sidebar / toggle terminal panel / new terminal |
| ⌘Z / ⇧⌘Z | Undo / redo |
| Tab / ⇧Tab, ⌘] / ⌘[ | Indent / outdent |
| ⌘/ | Toggle line comment |
| ⌥↑ / ⌥↓, ⇧⌥↑ / ⇧⌥↓ | Move line / copy line |
| ⇧⌘K / ⌘↵ / ⇧⌘↵ | Delete line / insert line below / insert line above |
| ⌘L / ⇧⌘\\ | Select line / go to bracket |
| ⌘= / ⌘- / ⌘0 | Zoom the whole window in / out / reset |
| ⇧⌘V | Toggle SVG preview |
| ⌥Z | Toggle word wrap for the current tab |

## License

[MIT](LICENSE)
