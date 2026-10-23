# Settings

Mayoi's preferences and keyboard shortcuts are two JSON files that you can edit with Mayoi or any other editor. When
you save one, the change applies in every window right away. You don't need to restart.

## Where the files are

All of Mayoi's files are in one data folder:

| System  | Folder                                                       |
| ------- | ------------------------------------------------------------ |
| macOS   | `~/Library/Application Support/Mayoi`                        |
| Linux   | `$XDG_CONFIG_HOME/Mayoi`, or `~/.config/Mayoi` if it's unset |
| Windows | `%AppData%\Mayoi`                                            |

| File                | Who writes it | What it holds                                                                     |
| ------------------- | ------------- | --------------------------------------------------------------------------------- |
| `settings.json`     | you and Mayoi | [Preferences](#settingsjson): fonts, theme, auto save, vim and more               |
| `keybindings.json`  | you           | [Keyboard shortcuts](#keybindingsjson), in VS Code's format                       |
| `state.json`        | Mayoi         | Windows, tabs, recent folders and layout                                          |
| `window-state.json` | Mayoi         | Each window's position and size                                                   |
| `updater.json`      | Mayoi         | Update choices: automatic downloads, a skipped version, the last check            |
| `sync.json`         | Mayoi         | [Settings sync](#settings-sync): the account, and the settings as last synced     |
| `crash.log`         | Mayoi         | Mayoi's version and system, then what ended it if it crashed, with every stack    |
| `crashes/`          | Mayoi         | The last 10 crash logs, by time; `.sent` or `.unsent` once asked about            |

To keep the files somewhere else, start Mayoi with `MAYOI_DATA_DIR` set to a folder. Mayoi then reads and writes all
of these files there instead, which is a way to try a setup without touching your own. Create the folder first, and
quit Mayoi before, since a Mayoi already running takes over a second one. On macOS:

```sh
mkdir -p ~/mayoi-test
MAYOI_DATA_DIR=~/mayoi-test /Applications/Mayoi.app/Contents/MacOS/Mayoi
```

## settings.json

**File → Open Settings** (⌘,) opens `settings.json` in a tab. Each key below is optional, and a key you leave out
takes its default.

| Key                     | Default       | Values                                                      | Also set by                                    |
| ----------------------- | ------------- | ----------------------------------------------------------- | ---------------------------------------------- |
| `fontFamily`            | `"monospace"` | Font names, separated by commas                             |                                                |
| `fontSize`              | `13`          | 9 to 28                                                     | ⌘= and ⌘- change it, and ⌘0 resets it to 13    |
| `fontWeight`            | `400`         | 100 (thin) to 900 (black)                                   |                                                |
| `fontFeatures`          | none          | A list of OpenType features                                 |                                                |
| `lineHeight`            | `1.55`        | 1 to 3, as a multiple of `fontSize`                         |                                                |
| `terminalFontFamily`    | `""`          | Font names, separated by commas                             |                                                |
| `terminalFontSize`      | `0`           | 0, or 9 to 28                                               |                                                |
| `terminalCopyOnSelect`  | `true`        | `true`, `false`                                             |                                                |
| `theme`                 | `""`          | `""` (Mayoi), `"matrix"`, `"claude"`, `"qoder"`, `"grokbot"` | **View → Theme**, **Color Theme…**             |
| `appearance`            | `""`          | `""` (follow the system), `"light"`, `"dark"`               | **View → Theme**                               |
| `sidebarRight`          | `false`       | `true`, `false`                                             | **View → Sidebar on Right**                    |
| `panelPosition`         | `""`          | `""` (bottom), `"left"`, `"right"`                          | **View → Panel Position**                      |
| `terminalLocation`      | `""`          | `""` (the panel), `"editor"`                                | **View → New Terminals in Editor Area**        |
| `projectTabs`           | `false`       | `true`, `false`                                             | **View → Project Tabs**                        |
| `projectTabStyle`       | none          | A label, color and icon for each folder                     | A square's context menu on the rail            |
| `locale`                | `""`          | `""` (follow the system), `"en"`, `"zh-Hans"`, `"ja"`       | **View → Language**                            |
| `autoSave`              | `true`        | `true`, `false`                                             | **File → Auto Save**                           |
| `wordWrap`              | `false`       | `true`, `false`                                             | **View → Word Wrap**                           |
| `markdownPreview`       | `false`       | `true`, `false`                                             | **View → Preview Markdown on Open**            |
| `vim`                   | `false`       | `true`, `false`                                             | **View → Vim Mode**                            |
| `vimCtrlKeys`           | `false`       | `true`, `false`                                             | Only this file                                 |
| `blame`                 | `true`        | `true`, `false`                                             | **Git → Inline Blame**                         |
| `diffLayout`            | `""`          | `""` (side by side when wide enough), `"split"`, `"inline"` | A diff's **Layout** button                     |
| `diffCrossFiles`        | `""`          | `""` (say it, and cross on one more step), `"always"`, `"never"` | Only this file                            |
| `diffHighlight`         | `""`          | `""` (changed words), `"line"`, `"char"`                       | A diff's **Highlight** menu                |
| `diffContext`           | `3`           | `0` to `100` unchanged lines around each change                | A diff's **Context** menu                  |
| `diffSyncScroll`        | `true`        | `true`, `false`                                                | A diff's **Synchronized Scrolling** menu   |
| `diffAlignedRows`       | `true`        | `true`, `false`                                                | A diff's **Layout** menu                   |
| `scmTree`               | `false`       | `true`, `false`                                             | **View as Tree** in Source Control's menu      |
| `scmAllBranches`        | `false`       | `true`, `false`                                             | **Show All Branches** in Source Control's menu |
| `syncIgnored`           | none          | Keys, `"settings.json"`, `"keybindings.json"`               | The [settings sync](#settings-sync) panel      |
| `aiModel`               | `""`          | `""` (`deepseek-flash`), or a model of the AI gateway       | Only this file                                 |
| `crashReports`          | `""`          | `""` (ask), `"always"`, `"never"`                           | **Don't ask again** when Mayoi asks           |

### Language

- **`locale`** is the language Mayoi shows: English, Simplified Chinese (`"zh-Hans"`) or Japanese (`"ja"`). Left
  empty, Mayoi shows the first of these that the system's preferred languages list, and English otherwise. Another tag
  of the same language works too, as `"zh"` or `"ja-JP"`. The menus, the command palette and every window change at
  once. The command palette also finds commands by their English names. In Japanese, the parts that MyGo draws itself,
  as the editor's own menu and the native dialogs' buttons, stay in English.

### Fonts

- **`fontFamily`** is the code's font. A list falls back in order, as `"Iosevka, monospace"`.
- **`fontWeight`** picks the closest of the weights the font has.
- **`fontFeatures`** turns OpenType features on and off: `["calt=0"]` turns off most coding ligatures, and `["ss01"]`
  turns a stylistic set on.
- **`terminalFontFamily`** and **`terminalFontSize`** are the terminal's. Left empty, the terminal uses `fontFamily`
  and half a point under `fontSize`. Zooming leaves a terminal size you set alone. The terminal also takes
  `fontWeight` and `fontFeatures`.

The editor lines up columns, indent guides and the caret in cells as wide as the font's `0`, so a font whose letters
differ in width lines them up only roughly.

### The rest

- **`theme`** is Mayoi's own theme, The Matrix, Claude or Qoder. The Matrix is always dark, so it ignores `appearance`.
- **`panelPosition`** is where the terminal panel is: `""` or `"bottom"` below the editors, `"left"` or `"right"` at a
  side of the window. At a side it sits past the sidebar, so with the sidebar on the same side the panel is the outer
  of the two.
- **`terminalLocation`** is where a new terminal opens: `""` in the panel, or `"editor"` in a tab of the editor area,
  in the column files open in, as **New Terminal in Editor Area** does. It applies to New Terminal (⌃⇧`) and the
  Explorer's **Open in Terminal**. Toggle Terminal (⌃`), the title bar's terminal button and the panel's **+** still
  open the panel.
- **`projectTabs`** keeps every folder open in one window, as project tabs on a rail on its left, rather than a
  window for each. Changing it gathers the windows into one, or parts them, at once.
- **`projectTabStyle`** gives a project's square on the rail its own label, up to two characters, in place of its
  initials or logo, and its own color, one of `"red"`, `"green"`, `"yellow"`, `"blue"`, `"magenta"` and `"cyan"`, by
  the project's folder: `{"/Users/me/Downloads": {"label": "DL", "color": "cyan"}}`. A remote folder's key is
  `/ssh:host/path`. `"icon"` names a picture in the `project-icons` folder beside `settings.json`, which **Choose
  Icon…** and dropping a picture on the square copy there, shown in place of the project's logo. It doesn't sync,
  as the folders and the pictures are this machine's.
- **`terminalCopyOnSelect`** copies what you select in a terminal to the clipboard as you let go of the pointer,
  and what Select All selects. Changing it applies to the terminals already open.
- **`autoSave`** saves a changed file a moment after you stop typing, and when you leave it.
- **`wordWrap`** wraps long lines at the edge of the editor. **⌥Z** wraps or unwraps one tab only, until this
  setting changes.
- **`markdownPreview`** opens Markdown files, .mdx included, rendered rather than as text. **⇧⌘V** still shows
  the text, and the choice of a tab is remembered for the session.
- **`diffHighlight`** chooses whether changed lines show word-level, character-level or line-level tinting. The
  highlighting changes how a diff looks; block actions still use whole line hunks.
- **`diffContext`** keeps that many unchanged lines around each diff hunk visible and folds the rest.
- **`diffAlignedRows`** keeps the rows of a split diff's sides aligned: a line the one side lacks holds a space on the
  other. Turn it off, and each side's lines close up where the other has lines it lacks, the sides reading in streams
  of their own, which scroll on their own whatever `diffSyncScroll` has.
- **`diffSyncScroll`** keeps the two sides of a split diff aligned while scrolling. Turn it off to scroll each side
  independently.
- **`vim`** starts editors in vim's normal mode.
- **`vimCtrlKeys`** gives ⌃F, ⌃B, ⌃R, ⌃A and ⌃X to vim's normal mode on Windows and Linux, rather than to Find,
  Toggle Sidebar, Open Recent, Select All and Cut. On macOS, where those commands use ⌘, vim always has them.
- **`blame`** shows who last changed the caret's line, and when, at the end of the line.
- **`scmTree`** lists Source Control's changes in a tree of their folders, rather than by path.
- **`scmAllBranches`** shows the commits of every branch and tag, not only those of the branch checked out.
- **`aiModel`** is the model of Mon's AI gateway that writes commit messages, `deepseek-flash` when empty. The models
  your account can use are listed at `https://mon.sorrycc.dev/api/v1/ai/v1/models`.
- **`crashReports`** is what Mayoi does at launch with the crash logs of the last runs: `""` asks whether to send them
  to Mon, `"always"` sends them and `"never"` doesn't. A report is anonymous, never sent with your account's token,
  and has your home folder taken out. **Show Report** in the question opens the logs as they would be sent. Only
  releases send reports, and builds started with `MAYOI_MON_URL`.

### Example

```json
{
  "fontFamily": "JetBrains Mono, monospace",
  "fontSize": 14,
  "fontFeatures": ["calt=0"],
  "lineHeight": 1.6,
  "terminalFontSize": 12,
  "theme": "claude",
  "vim": true
}
```

### Good to know

- **It's plain JSON.** Comments and trailing commas don't parse, though `keybindings.json` allows comments.
- **Mayoi rewrites the file** in its own formatting soon after it starts, when you open it with ⌘, and when you
  change a preference from a menu, a button or zoom. It writes every key, defaults included, and drops any key it
  doesn't know.
- **Mayoi ignores a file that doesn't parse,** and says nothing. The preferences stay as they were until you fix it
  and save again.
- **Mayoi quietly corrects values out of range.** A `fontSize` of 40 becomes 28, and an empty `fontFamily` becomes
  `"monospace"`.
- **Older files still work.** Themes used to be appearances, so Mayoi reads `"appearance": "matrix"` or `"claude"`
  as that `theme`.

## Files Mayoi keeps itself

`state.json`, `window-state.json` and `updater.json` are Mayoi's memory, not settings. Mayoi writes them as it runs,
so it overwrites any change you make to one while it's open. To start fresh, quit Mayoi and delete the file.

- **`state.json`** holds the open windows with their folders, columns and tabs, each tab with its caret, selection,
  scroll position and own word wrap. It also holds the folders open in the Explorer, the recent folders and the sessions
  Mayoi restores when one opens again, and each window's layout: its sidebar's view, whether it is open and its width,
  and its panel's height and, at a side, its width. Deleting it forgets all of that.
- **`window-state.json`** holds each window's position and size.
- **`updater.json`** holds your choices in the update window. See [Updates](FEATURES.md#updates).

Versions before 0.5.1 kept the windows and layout in `settings.json`. Mayoi moves them to `state.json` the first time
it starts.

## Settings sync

**Mayoi → Sign In…** (**Help → Sign In…** on Windows and Linux, or **Account: Sign In…** in the command palette) signs in to your account at [mon.sorrycc.dev](https://mon.sorrycc.dev).
Mayoi shows a code and opens it in the browser: sign in there with GitHub or Google, check that the page shows the same
code and approve it. The dialog counts down until the code expires.

What syncs:

- **`settings.json`**: every preference, except `scmRepositoryOpen` and `scmRemotes`, which remember Source Control's
  panels on each machine, `projectTabStyle`, kept by this machine's folders, and `syncIgnored`. When two machines change different preferences, both changes stay. When
  they change the same one, the machine that syncs last wins.
- **`keybindings.json`**, as text, comments and all. When it changed on this machine and on another since they last
  synced, this machine's stays and the other's is saved beside it as `keybindings.sync-conflict.json`. A toast and the
  account's panel offer **Compare**, which opens the two side by side to merge by hand, and **Keep Mine**, which
  deletes the other's.

The windows, tabs and recent folders (`state.json`), window positions and update choices stay on each machine.

### The account's panel

Signed in, a cloud at the right of the status bar shows how settings sync is going: a check once it is up to date, a
spinner while it syncs, a crossed-out cloud when it couldn't reach the account, and a warning while a
`keybindings.sync-conflict.json` waits to be merged. Click it, or run **Account: Show Settings Sync**, for the
account's panel. The panel shows:

- who you are signed in as;
- when this machine last synced, or why it couldn't and when it tries again, with a button to sync now;
- a switch each for **Settings** and **Keyboard Shortcuts**, to keep them on this machine;
- **Manage Account**, which opens mon.sorrycc.dev, and **Sign Out**.

### Keeping settings on one machine

`syncIgnored` lists what stays on this machine, which the switches of the account's panel set too:

```jsonc
{
  // This laptop keeps its own font size and keyboard shortcuts.
  "syncIgnored": ["fontSize", "keybindings.json"]
}
```

An entry is the key of a preference, `"settings.json"` for every preference, or `"keybindings.json"` for the keyboard
shortcuts. What a machine keeps stays as it has it there, and the account keeps its own for the other machines. Take an
entry out, and the machine's value syncs from its next change. `syncIgnored` itself doesn't sync, so each machine has
its own.

### When it syncs

A change saves to the account about two seconds after you make it, whether from a menu or by saving the file. A change
made just before you quit saves as Mayoi quits, waiting three seconds at most. Mayoi fetches the account's changes as it
starts, as it comes to the front, and with **Sync Settings Now**. Without a connection, nothing is lost: Mayoi tries
again after 10 seconds, then waits longer each time, up to five minutes, and the next sync merges what changed.

The first time a machine signs in, a machine with Mayoi's default settings takes the account's, and an account with
none takes the machine's. When both have settings of their own, Mayoi asks once which to keep.

**Sign Out of Settings Sync…** asks first, then stops syncing and signs the machine out. The settings stay as they are
on the machine and in the account. Each machine signed in shows on your account page at mon.sorrycc.dev, where you can
sign one out or delete the settings saved there.

The sign-in is kept in the system's credential store: the Keychain on macOS, the Credential Manager on Windows, and the
Secret Service on Linux (GNOME Keyring, KWallet and others). On Linux without one, settings sync is not available.
Development builds (`mygo dev`, `go run`) keep it in `mon-token` beside settings.json instead, readable only by you, as
the Keychain would ask again after every rebuild.

## keybindings.json

**File → Open Keyboard Shortcuts** opens `keybindings.json`, beside `settings.json`, in VS Code's format, comments and
all, so a VS Code or Cursor file can be pasted in. Saving it applies it in every window at once. A file that does not
parse is left alone until it does, and a key or a condition Mayoi can't read is named in a toast.

- **`key`** is a key, as `"cmd+1"`, or a chord, as `"ctrl+. g c"`. Its modifiers are `cmd`, `ctrl`, `alt` and `shift`.
  While a chord is waiting for its next key, the status bar shows its keys. A key it has no binding of does nothing and
  ends it, and Escape ends it too.
- **`command`** is a command of Mayoi's, by its id as `view.sidebar`, or VS Code's name for it, as
  `workbench.action.toggleSidebarVisibility`. A command named with a `-` first takes the key away from that command.
  `runCommands` runs its `commands` in order, each once the edits and the Git commands of the one before are done, and
  stops at an error. `vscode.open` opens the file of its `args`: an absolute path, one from `~`, or one in the folder.
  `type` types the `text` of its `args` in the editor or the terminal, and `workbench.action.terminal.sendSequence`
  sends it to the terminal. Pressing the key of a command Mayoi doesn't have says so.
- **`when`** is when it applies: `editorTextFocus`, `editorFocus`, `inputFocus`, `textInputFocus`,
  `filesExplorerFocus`, `terminalFocus`, `terminalHasBeenCreated`, `terminalIsOpen`, `sideBarVisible`,
  `editorHasSelection`, `isMac`, `isLinux` and `isWindows`, with `!`, `&&`, `||` and parentheses. Other names don't hold.

The keys of the file take the place of Mayoi's own: the menus drop a key the file binds, ⌘1 to ⌘9 and ⌃R let the file
have theirs, and the command palette shows the file's keys. The shortcuts apply in the editor, the terminal, the
Explorer, diffs, pictures, and the text fields of find, search, quick open and the commit message.

```json
[
  { "key": "ctrl+. s", "command": "editor.action.indentUsingSpaces" },
  { "key": "ctrl+. g p", "command": "git.push" },
  { "key": "d", "command": "deleteFile", "when": "filesExplorerFocus && !inputFocus" },
  { "key": "cmd+1", "command": "workbench.action.toggleSidebarVisibility" }
]
```

### Windows and Linux shortcuts

The shortcuts are macOS's with Ctrl for ⌘, and the hints show them so, but for those VS Code has others for:

| Command                                  | macOS    | Windows and Linux |
| ---------------------------------------- | -------- | ----------------- |
| Find Next, Find Previous                 | ⌘G, ⇧⌘G  | F3, Shift+F3      |
| Replace                                  | ⌥⌘F      | Ctrl+H            |
| Show the tab 1 to 8, the last            | ⌘1 to ⌘9 | Alt+1 to Alt+9    |
| Save All, Open Folder in New Window      | ⌥⌘S, ⌥⌘O | none              |
| Focus Next Column, Focus Previous Column | ⌥⌘→, ⌥⌘← | none              |

Ctrl with Alt is AltGr there, which types text, so no shortcut takes it: bind those commands in `keybindings.json`,
as `{ "key": "ctrl+k s", "command": "workbench.action.files.saveAll" }`. A file can tell the systems apart with
`isWindows`, `isLinux` and `isMac`.
### `diffWhitespace`

Controls which whitespace participates in diff comparison: `""` for exact,
`"eol"` to ignore line-end spaces and tabs, `"all"` to ignore whitespace,
or `"blank"` to also treat whitespace-only lines as blank. The filter changes
the diff display and navigation while file text and block actions keep using
the original lines.
