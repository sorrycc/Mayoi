<p align="center">
  <img src="resources/icon.png" width="128" height="128" alt="Mayoi icon">
</p>

<h1 align="center">Mayoi</h1>

<p align="center">迷い — a small, fast code editor written in Go. It draws a native UI on the GPU with
<a href="https://github.com/egoist/mygo">MyGo</a>, with no webview and no Electron, and the packaged app is about 18 MB.</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/screenshot-dark.png">
  <img alt="Mayoi editing its own source in two columns" src="docs/screenshot-light.png">
</picture>

## Features

- **Editor:** tabs, Chroma syntax highlighting, indent guides, sticky scroll, breadcrumbs and word wrap
- **Smart editing:** auto-indent, auto-closing brackets and quotes, line commands, and per-tab undo
- **Vim mode:** normal, insert and visual modes, with motions, operators, text objects, counts and `.`
- **Columns:** split the editor side by side and drag tabs between columns
- **Explorer:** a live file tree that follows the disk and dims Git-ignored files
- **Quick open:** fuzzy file search, go to line (`:42`), go to symbol (`@`) and a command palette (`>`, ⇧⌘P)
- **Find and replace:** case, whole-word and regex matching, searched in the background in large files
- **Search:** find and replace across the folder's files (⇧⌘F, ⇧⌘H), with files to include and exclude, and a
  preview of each replacement
- **Git:** a source control view to stage, commit, amend, undo, pull and push, every change in one diff, folded
  around the changes, inline or side by side, a graph of the commits with their branches and tags, each with its own
  diff, gutter marks that revert or stage a change, branches and blame
- **Remote SSH:** open remote folders over SFTP, edit and atomically save files, run Git on the remote host, and open SSH terminals using your existing OpenSSH configuration
- **Terminal:** Ghostty's emulator (libghostty-vt), in the bottom panel or in a tab
- **Image viewer:** PNG, JPEG, GIF, WebP and BMP, plus a live SVG preview
- **Auto save,** light, dark, The Matrix and Claude themes, one window per folder, and a `mayoi` shell command
- **Settings:** a JSON file applied as you save it, with the code's and the terminal's fonts, weight, ligatures and
  line height

See [docs/FEATURES.md](docs/FEATURES.md) for the details.

## Requirements

- macOS 13 or later
- Go 1.27 or later

Mayoi also builds for Linux and Windows.

## Getting started

```sh
go run ./cmd/mayoi ~/some/project    # open a folder or a file
go tool mygo build                   # build build/darwin-arm64/Mayoi.app and a .dmg
```

To open folders from a shell, choose **File → Install 'mayoi' Command**, then run `mayoi .` or `mayoi <file>`.

## Remote SSH

Choose **File → Connect via SSH…** (also on the welcome screen). Enter just an
SSH host alias, `user@hostname:port`, or a complete SSH URL:

```text
my-ssh-alias
user@hostname:2222
ssh://my-ssh-alias/home/me/projects
```

After connecting, **Open Remote Folder** lists remote directories. A host without
a path starts in the remote home directory. Click folders to browse, use **Up**
or **Home**, or enter an absolute path and press **Go**. Then choose **Open Folder**
to make the current directory your project, or **New Window** to keep the existing
project open.

Inside an SSH workspace, **⌘O / File → Open Folder…**, the sidebar's always-visible
**Open Remote Folder…** button, and **File → Open Remote Folder…** reopen this browser
on the same host. You can also right-click an Explorer folder and choose **Open as
Project**. Switching projects follows the regular unsaved-changes flow and closes
the previous project's terminals. **File → Open Local Folder…** still opens the
native local folder chooser.

The host can be an alias from `~/.ssh/config`. OpenSSH handles keys, SSH Agent
(including 1Password via `IdentityAgent`), ProxyJump and host verification.
First connect with `ssh my-ssh-alias` in a terminal to verify the host key and
confirm key/agent authentication works. File access uses batch authentication;
password-only connections are not supported by the file browser.

The remote machine needs an SFTP subsystem; Git features additionally need
`git` and a POSIX shell. No Mayoi server, filesystem mount or project download
is required. Files, Git commands and terminals all operate on the remote host.
Saves use a temporary sibling file and atomic rename; a failed upload keeps the
buffer dirty and preserves the original file. Existing-file replacement requires
the OpenSSH POSIX rename extension. Remote directories and open-file metadata
refresh every three seconds; dirty buffers are not replaced by external changes.

Remote workspaces appear in **Open Recent** and restore on launch. After a
connection failure, use **File → Reconnect SSH** or click the SSH status label.
**Save As** asks for a path on the same remote host. Remote Trash/Finder actions
are unavailable; use the SSH terminal for file deletion. This first version
supports POSIX remote hosts and may wait for the network when opening a file.

After installing the shell command, `mayoi ssh://my-ssh-alias/home/me/project`
opens a remote folder. The app binary accepts the same URL as an argument.

## Development

```sh
scripts/dev.sh                                              # run Mayoi Dev with live reload, with its own settings
go test ./...                                               # headless UI tests
go test -short ./...                                        # skip the tests that start a shell
go test -short -run XXX -bench Frame ./cmd/mayoi            # benchmark an editor frame
SHOTS=$PWD/docs go test -run TestShotReadme ./cmd/mayoi     # redraw the README screenshots
go run ./tools/icongen                                      # redraw resources/icon.png
```

## Releasing

Write the release notes under `## Unreleased` in `CHANGELOG.md` and commit them, then:

```sh
scripts/bump.sh patch    # or minor, major, or a version such as 0.2.0
```

It sets the version in `mygo.json`, renames `## Unreleased` to `## <version>` in `CHANGELOG.md`, commits and tags `v<version>`, and after asking pushes `main` and the tag. The
[Release workflow](.github/workflows/release.yml) then builds macOS (arm64 and Intel, signed and notarized), Windows and
Linux, uploads them with their signed updates, and publishes the release. Installed apps check for it once a day, and
from **Mayoi → Check for Updates…**; the update window shows the changelog section.

The workflow needs these repository secrets:

| Secret | What |
|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | `mygo-update.key` from `go tool mygo keygen`, whose public key is in `mygo.json` |
| `MACOS_CERTIFICATE` | the Developer ID Application certificate and its key, as a base64 `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | the password of the `.p12` |
| `APPLE_ID`, `APPLE_TEAM_ID` | the Apple ID of the developer account, and its team ID |
| `APPLE_APP_PASSWORD` | an app-specific password of the Apple ID, from account.apple.com |

Local builds stay signed ad hoc; only the workflow signs with the Developer ID. Windows builds are not signed.

## Keyboard shortcuts

| Shortcut | Action |
|---|---|
| ⌘P / ⇧⌘O / ⌃G | Quick open / go to symbol / go to line |
| ⌘F / ⌥⌘F / ⌘G | Find / replace / next match |
| ⇧⌘F / ⇧⌘H | Find in files / replace in files |
| ⌘S / ⇧⌘S / ⌥⌘S | Save / save as / save all |
| ⌘O / ⌥⌘O | Open folder / open folder in a new window |
| ⌘N / ⌘W / ⌘1…9 / ⌃Tab | New file / close tab / go to tab / next tab |
| ⌘\\ / ⌥⌘→ / ⌥⌘← | Split right / next column / previous column |
| ⌘B / ⌃\` / ⌃⇧\` | Toggle sidebar / toggle terminal panel / new terminal |
| ⌘Z / ⇧⌘Z | Undo / redo |
| Tab / ⇧Tab, ⌘] / ⌘[ | Indent / outdent |
| ⌘/ | Toggle line comment |
| ⌥↑ / ⌥↓, ⇧⌥↑ / ⇧⌥↓ | Move line / copy line |
| ⇧⌘K / ⌘↵ / ⇧⌘↵ | Delete line / insert line below / insert line above |
| ⌘L / ⇧⌘\\ | Select line / go to bracket |
| ⌘= / ⌘- / ⌘0 | Zoom text or image in / out / reset |
| ⇧⌘V | Toggle SVG preview |
| ⌥Z | Toggle word wrap for the current tab |

## Architecture

Mayoi is a single Go program. MyGo provides the windows, the GPU renderer and the text area. Chroma highlights code on a
background goroutine, libghostty-vt runs the terminal, and fsnotify keeps the file tree and the quick-open index current.
Undo history, editing commands and decorations belong to the app rather than the text area, so every column showing a
file shares its text and history.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for a tour of the source.

## Performance

These figures come from the tests, run on an M-series Mac:

- A 100,000-line file opens in about 20 ms.
- Scrolling and typing cost under 2 ms per frame, or about 3 ms with find open.
- Highlighting again after a keystroke in a 6,000-line file takes under 1 ms.
- Quick open searches 100,000 paths in about 1.5 ms.

## License

[MIT](LICENSE)
