# Features

## Explorer

A lazy file tree that follows the disk, with Git-ignored files shown faint. You can create, rename, trash and reveal files
from it. In a Git work tree, a changed file shows its letter (M, A, D, U for untracked, C for a conflict) and its name in a
color, and a folder with changes inside gets a dot. While the pointer is over the sidebar, the folder's row at the top
has New File, New Folder, Refresh and Collapse Folders.

A click on the space below the files, or Escape, unselects the file or folder chosen, so new files go in the workspace's
folder. A right click there opens the workspace's menu: New File…, New Folder…, Reveal in Finder, Open in Terminal and
Paste.

## Editor

Tabs, line numbers, a highlighted caret line, and Chroma syntax highlighting. Files changed on disk reload unless you
have unsaved edits.

A file clicked in the Explorer, or chosen in search's results, opens in a preview tab, its name in italics: the next
file chosen takes its place, so looking through files leaves one tab. Editing it, double-clicking it or its tab,
pressing Enter on it, or dragging it to another column keeps it open. A file already in a tab shows there. Each column
has its own preview tab, which comes back as one at the next start. A commit clicked in source control opens in the
same preview tab.

⇧⌘T reopens the last tab you closed, in its column and place, with its caret and scroll position. Press it again for
the one before. Files and terminals both come back, a terminal as a new shell in the folder it started in. Opening or
closing a folder forgets the tabs closed.

### Word wrap

Long lines scroll sideways by default. **View → Word Wrap** wraps them at the edge of the editor. ⌥Z toggles wrapping
for the current tab only, until the setting changes again.

### Auto save

A changed file saves:

- one second after the last edit
- when its column switches to another tab, or the keyboard moves to another column
- when the window loses focus

Closing a changed file saves it without asking. Untitled files wait for ⌘S. Turn this off from **File → Auto Save**.

### Editing

- Tab and ⇧Tab indent and outdent, using tabs or spaces to match the file.
- Enter keeps the indent, and indents one more level after an opening bracket.
- Brackets and quotes close themselves, wrap a selection, and are stepped over when typed again.
- Commands comment lines, move, copy and delete lines, and insert a line above or below.
- Home goes to the first non-blank character of the line.
- The status bar shows the file's indentation and line endings, and you can change either from there.

### Vim mode

**View → Vim Mode** edits with vim's keys. Each tab has its own mode, shown in the status bar, and starts in normal
mode, where the caret is a block.

- **Motions:** `h j k l`, `w b e ge` and `W B E gE`, `0 ^ $ g_`, `gg G`, `f F t T` with `;` and `,`, `%`, `{ }`, Enter
  and `-`. Arrows, Home and End move too. A count goes before any of them.
- **Operators:** `d c y`, `> <`, `gc` to comment, and `g~ gu gU` for case. They take a motion or a text object (`iw
  aw iW aW`, `i" a"` and the other quotes, `i( a(`, `i[`, `i{`, `i<`), or apply to lines when typed twice, as `dd`.
- **Commands:** `x X D C Y s S r J ~ p P o O`, `i a I A` to insert, `u` and ⌃R to undo and redo, `.` to repeat the
  last change, ⌃D ⌃U ⌃F ⌃B to scroll by half a view or a view, and ⌃E ⌃Y by a line.
- **Visual mode:** `v` selects characters and `V` lines. A selection made with the mouse is one too. Motions extend it,
  `o` swaps its ends, and operators, `r`, `J`, `~ u U` and `p` act on it.

Esc, ⌃[ or ⌃C leaves insert mode. A change and the text typed after it undo as one step. Yanking, deleting and pasting
use the system clipboard, and a yank of whole lines pastes as lines. In normal mode, ⌃R redoes rather than opening
recent folders. ⌘ shortcuts work as usual in every mode. Punctuation keys are read as a US keyboard types them. On
Windows and Linux, ⌃F, ⌃B and ⌃R stay Find, Toggle Sidebar and Open Recent in normal mode, unless `vimCtrlKeys` is on.

### Undo

Each tab keeps its own history. It covers the app's edits as well as typing, so indenting, moving lines, replacing and
reloading from disk can all be undone. Undoing back to the saved text marks the tab clean again.

### Reading aids

- Indent guides, with the caret's block drawn brighter.
- The bracket matching the one at the caret is boxed.
- Other occurrences of the word at the caret are highlighted.
- The selection has rounded corners and shows its spaces and tabs. It stays visible, dimmed, while the keyboard is
  elsewhere.
- Sticky scroll pins the lines that open the blocks in view to the top, and breadcrumbs name the blocks around the
  caret. Click either to jump there.

### Git

The gutter marks lines added, changed or removed since the last commit. A strip along the right edge shows these
changes, find matches and the caret position for the whole file.

Click a mark to see the lines as they were at HEAD, in a box under the change. From there you can revert the change,
which is a step of undo, or stage it, which puts only those lines in the index. ⌥F5 and ⇧⌥F5 go to the next and previous
change, and the **Git** menu has the same commands for the change at the caret.

The caret's line shows who changed it last, when, and the commit's summary, a moment after the caret stops. Turn this
off from **Git → Inline Blame**.

## Source control

The button beside **EXPLORER** at the top of the sidebar (⌃⇧G) switches it to the changes of the folder's Git work
tree, with a count of them. Files are grouped as merge changes, staged changes, changes and untracked files.

- The branch at the top opens **Switch Branch…**. Beside it, fetch, pull and push, which show how many commits
  they would bring down or send up; a branch with no upstream gets **Publish Branch** instead.
- Click a file, or press Enter, to see its changes with the diff focused, so its keys (`]` `[`, `.` `,`) work at once.
  Space stages or unstages the file chosen.
- The buttons on a row open the file, discard its changes, and stage or unstage it. A group's header does the same for
  all its files. Discarding asks first, and moves untracked files to the Trash.
- Write a message and press ⌘↵ or the commit button, which says what it will do: **Commit**, **Commit All** when nothing
  is staged (Mayoi asks before staging everything), or **Amend Commit**. Changed files save first. Past 50 characters
  the summary line shows its length, in a warning color past 72.
- The arrow beside the commit button has **Commit & Push**, **Amend Last Commit** (which fills in the last message) and
  **Undo Last Commit**, which takes the commit back but keeps its changes staged and its message in the box.
- The **⋯** menu by the title has the commit commands, stage, unstage and discard all, fetch, pull and push, Refresh,
  and **View as Tree**, which lists each group's files under their folders. A folder with only one folder in it shows as
  one row, as `internal/git`, and a folder's buttons stage, unstage or discard every file in it.
- A thin bar under the title moves while a Git command runs. A toast says when a commit is made, with an **Undo**
  button, and when a push or a pull is done.
- A clean work tree says so, and offers to push or pull when the branch is ahead of its upstream or behind it.
- A folder outside a repository gets an **Initialize Repository** button.

### Commits

Under the changes, **COMMITS** lists the last 50 commits of the branch, newest first, on a graph of their branches: a
branch merged in runs in a lane and color of its own beside the one it joined. Each commit shows its author's initials,
its branches and tags, and how long ago it was made. A ring marks HEAD, a small hollow dot a merge, and an arrow the
commits not pushed yet. A local branch and its remote's at the same commit share one badge, as `main ⇄ origin`; badges
that do not fit the sidebar's width fold into a count. **Show More Commits** reads 50 more. Point at a commit to see its
author, hash and every branch and tag.

The branch button in the COMMITS header, or **Show All Branches** in the **⋯** menu, shows every branch, of the remotes
too, and every tag, not only the history of the branch checked out. The button stays in the header, in the accent
color, while it is on.

Click a commit to see it in the preview tab, the keyboard staying in the list, so clicking through commits leaves one
tab; Enter, a double click or **Open Changes** in its menu keeps it in a tab of its own, focused. The tab shows its
message, author, date, hash, branches and tags at the top, then each file it changed from its first parent, as the
**Changes** tab shows them. Its menu checks it out (switching to
its branch when it has one, or detaching HEAD at it once asked), creates a branch or a tag there, copies the hash, the
message or the name of a branch or tag, and on HEAD undoes it.

### Diffs

Every file changed shows in one **Changes** tab, as source control lists them: the staged changes first, from HEAD
to the index, then the rest, from the index to the work tree. Each file is under a header with its path and the lines
added and removed. Click a file in source control and the tab scrolls to it; **Open Changes** from the Explorer, a
tab's menu or the **Git** menu does the same for a file. As you scroll, source control chooses the file you are
reading. The diff follows a file as you type in it.

Each header shows the file's icon, its path, a badge of its state (where a renamed file came from, too), and the lines
added and removed, in numbers and in a bar of five blocks. Click a header to fold its file; a file with more than 2,000
changed lines starts folded. Point at a header, or choose
it, for buttons that open the file, stage or unstage it, and discard its changes. Files are read as they come near the
view, so a long list of changes opens at once.

Only three unchanged lines show around each change. The rest fold into a row that says how many lines it hides, and a
click opens it. Each run of changes starts with an `@@` header naming the lines and the block it is in, as Git writes
it. **Show Full Files** in the breadcrumbs shows every line.

Point at a run of changes, or choose a line of it, and its header offers to stage those changes alone, to unstage them
when they are staged, or to discard them. Mayoi reads the index and the file again first, so it acts on them as they
are. Discarding edits an open file as a step of undo, and asks first for one that is not open. As you scroll through a
file, its header stays at the top of the view with its buttons, and a click there folds the file.

Removed and added lines show in one column, or side by side with the lines of a change across from each other. The
layout button in the breadcrumbs chooses side by side, one column, or side by side when each side has room for 80
columns, which is the default. Words that changed within a line are tinted,
and a bar along the left is brightest for the change you are on. Long lines wrap as the editor's do: **Word Wrap**
sets them all, and ⌥Z or the wrap button just the tab.

Double-click a line to open the file there. The arrows go from change to change, and the status bar counts the lines
added and removed. With the diff focused, keys work as in a pager:

| Key | Does |
| --- | --- |
| `j` `k`, ↓ ↑ | Choose the next or previous row |
| `]` `[` | Go to the next or previous change |
| `.` `,` | Go to the next or previous file |
| Space, `f`, `b`, `d`, `u` | Scroll a page down or up, or half a page |
| `g` `G` | Go to the top or the bottom |
| `z` | Open the folded lines chosen, or fold them again |
| Enter | Open folded lines, fold a file, or open the file at a line |
| `e` | Open the file at the chosen line |
| `s` | Stage the changes of the chosen line or run, or unstage them |

### Branches and remotes

The branch in the status bar shows how far it is ahead of its upstream (↑) and behind it (↓). Click it to switch
branches, create one, or fetch, pull and push. Switching waits until open files are saved.

**Switch Branch…**, from the branch at the top of source control, the status bar's menu or the **Git** menu, opens a box
of the branches, local and of the remotes, the one checked out first and then the one committed to last. Each shows the
subject of its last commit and how long ago it was made. Type to find one by name, or type a new name to create a branch
from the one checked out. Choosing a remote's branch checks out a local branch that tracks it.

Pull only fast-forwards. Push sets the upstream the first time. Git never prompts for a password inside Mayoi: it uses
your SSH agent and credential helper, and if those fail, the error says to run the command once in the terminal.

## Image viewer

PNG, JPEG, GIF, WebP and BMP files open in a viewer, fitted to the view on a checkerboard.

- ⌘= and ⌘- zoom, as does scrolling with ⌘ held. ⌘0 fits the image again.
- Double-click toggles between fitted and actual size.
- Drag or scroll to pan an image larger than the view.
- The image reloads when the file changes.

An SVG opens as text. ⇧⌘V, or the button at the end of the breadcrumbs, shows it as an image that updates as you type.

Markdown (`.md` and `.markdown`) opens as text too. ⇧⌘V or the same button shows a read-only, scrollable preview with headings, formatted text, links, nested lists, task lists, quotes, highlighted code, tables and local images. Split with ⌘\ to keep source in one column and a live preview in the other, or choose Open Preview to the Side, in the View menu or from the button beside Split Right, to open it so in one step; the source and its preview then scroll together, block by block, the one under the pointer leading. Preview state is restored with the session. Remote images show their alt text without network requests. A path starting with `/` is taken from the root of the file's Git repository, or else of the workspace, as GitHub takes it. HTML comments and front matter are hidden, `<br>` breaks the line, and other HTML is displayed as text.

## Columns

The editor splits into columns side by side, each with its own tabs, and you resize them by dragging the handle between
two columns. A file can show in several columns at once. Those columns share its text and undo history, and each keeps
its own caret and scroll position. Drag a tab along its strip, into another column, or to the edge of a column to give it
a new column.

Files dragged from the Finder open where you drop them: in a column after the tab shown, on its tab strip where the
marker shows, or on the edge of a column in a new column there. Dropped elsewhere, as on the explorer, they open in the
column that has the keyboard. A dropped folder opens as `mayoi <dir>` opens it.

Files dragged from the explorer, from source control's changes or from the search results open in the same places. A
search match opens with the match selected.

## Terminal

Ghostty's emulator (libghostty-vt), in the bottom panel or in a column tab. You can run several terminals, move one
between the panel and a column by dragging it or from its menu, and maximize the panel. Terminal colors follow the theme.
Files dropped on a terminal type their paths, quoted for the shell.

On Windows and Linux the shell has the keys with Ctrl, which edit its line, except the workbench's: Quick Open, the
Command Palette, the terminal and the sidebar toggles, Next and Previous Tab, Alt+1 to Alt+9 and zoom, and every
Ctrl+Shift shortcut but Ctrl+Shift+C and Ctrl+Shift+V, the terminal's copy and paste, and those of the function keys.

## Quick open

⌘P, or the search box in the title bar, opens a fuzzy file search with recent files first. In a Git work tree it lists
the files Git tracks, so build output stays out.

- Type `:42` to go to line 42.
- Type `@` to list the file's functions, types and headings.
- Type `>`, or press ⇧⌘P, to run any command by name. Each shows its menu, its shortcut and a check when it is on,
  and the commands run last come first.

## Find and replace

⌘F finds and ⌥⌘F replaces, with options for match case, whole word and regular expressions (`$1` refers to a group). In
large files, matches are found in the background as you type.

## Search

The button beside **EXPLORER** with a magnifying glass, ⇧⌘F or **Edit → Find in Files** searches every file of the
folder, as quick open lists them, with options for match case, whole word and regular expressions. The selection, when
it is on one line, becomes the query. Results come in as they are found, a line for each match under a header for its
file, with its line number; click a header to fold it, or **Collapse All** in the title. Click a match, or move to it, and
the editor shows it, selected, in the preview tab, while the keyboard stays in the results; Enter or a double click
keeps it open in a tab and moves the keyboard to it. ↓ goes from the query to the results, where ↓ and ↑ move from
match to match; ↑ on the first match, or Escape, goes back to the query. A thin bar under the title moves while a search runs.

- Files with unsaved edits are searched as they are in the editor, and the results follow a file as you type in it or
  it changes on disk.
- Binary files and files over 4 MB are passed by, and the search stops at 20,000 matches.
- **⋯** shows **files to include** and **files to exclude**: patterns separated by commas, such as `*.go, web/**`.
  `*` and `?` match within a name, `**` any folders, and `{ts,tsx}` either. A pattern without a slash, as `*.ts` or
  `testdata`, matches a name anywhere; one with a slash matches from the folder's top. A folder's pattern takes the
  files in it.

### Replace

The arrow beside the query (⇧⌘H, **Edit → Replace in Files**) shows the replacement. Each match then shows struck out,
with what replaces it beside it. With regular expressions, `$1` refers to a group.

- **Replace All**, beside the replacement, asks first, then replaces every match.
- The button on a file's header replaces its matches, and the one on a match replaces it alone.
- A file open in the editor changes as one step of its undo. A file not open is written at once, keeping its line
  endings and permissions, and that cannot be undone, which Replace All says before it starts.
- Matches are found again in the text as it is when you replace. A single match that moved since the search is left
  alone, with a message to search again.

## Windows

Each folder gets its own window.

- **Open Folder…** (⌘O) opens a folder in the current window.
- **Open Folder in New Window…** (⌥⌘O), or ⌘-clicking a recent folder (or pressing ⌘↵ on it), opens it in a new window.
- **File → Open Recent** lists the last ten folders, except the window's own and any no longer on disk, and has
  **Clear Recently Opened**. On macOS the Dock icon's menu lists them too.
- ⌃R (or **Open Recent → More…**) finds a recent folder by name: ↵ opens it in the current window, ⌘↵ in a new window.
  When a terminal has the keyboard, ⌃R goes to the shell instead.
- Opening a folder that another window already has brings that window to the front instead.

## The `mayoi` command

**File → Install 'mayoi' Command** installs the command into `~/.local/bin` when that directory is on your `PATH`, and
otherwise into `/usr/local/bin`, which asks for your password.

- `mayoi .` opens the current folder, in a new window or the one that already has it.
- `mayoi <file>` opens a file in the window whose folder contains it.

The command returns immediately, and starts Mayoi if it isn't running.

## Appearance and persistence

**View → Theme** picks a theme, Mayoi, The Matrix or Claude, and apart from it whether the window follows the system's
light or dark mode or is always Light or Dark. Light and Dark are greyed out under The Matrix, which is always dark.
The Matrix is phosphor green on black, with the whole window in the code's font, file icons in greens, and red and amber
kept for what is removed, failed or changed. Digital rain falls behind the welcome page while the window has the
keyboard, and holds still without it or when the system asks for reduced motion. Claude is Anthropic's ivory and warm
near-black with Claude's orange, in a light and a dark kind. Clawd, Claude Code's character, takes the logo's place on
the welcome page and sits at the start of the status bar: it blinks now and then, and hops while a Git command or a search of the files
runs, and holds still as the rain does. **View → Sidebar on Right**
moves the sidebar to the right of the editors. It remembers the sidebar's side, the sidebar and panel sizes, and every
window with its folder, frame, columns and tabs, each tab with its caret, selection, scroll position and own word wrap,
and the folders open in the Explorer. Started without a folder, Mayoi reopens the windows that were open when it quit.
A recent folder closed, by closing its window, **File → Close Folder** or opening another folder in its window, opens
again with its tabs, columns and Explorer as they were. Closing the last window quits the app.

## Settings

**File → Open Settings** (⌘,) opens `settings.json` in a tab. Saving it applies its preferences in every window at once:
the fonts, `theme`, `appearance`, `autoSave`, `wordWrap`, `diffLayout`, `scmTree`, `scmAllBranches`, `blame`, `vim`,
`vimCtrlKeys` and `sidebarRight`. A file that does not parse is left alone until it does. Mayoi keeps the windows, tabs, recent folders
and layout itself, in `state.json` beside it.

The font keys:

| Key                  | Default       | What it does                                                                      |
| -------------------- | ------------- | --------------------------------------------------------------------------------- |
| `fontFamily`         | `"monospace"` | The code's font. A comma-separated list falls back in order, as `"Iosevka, monospace"`. |
| `fontSize`           | `13`          | 9 to 28. ⌘= and ⌘- change it, and ⌘0 sets it back to 13.                          |
| `fontWeight`         | `400`         | 100 (thin) to 900 (black), of the weights the font has.                           |
| `fontFeatures`       | none          | OpenType features: `["calt=0"]` turns off most coding ligatures, `["ss01"]` a stylistic set on. |
| `lineHeight`         | `1.55`        | The height of a line of code, a multiple of `fontSize`, from 1 to 3.              |
| `terminalFontFamily` | `""`          | The terminal's font, else `fontFamily`.                                           |
| `terminalFontSize`   | `0`           | The terminal's size, else half a point under `fontSize`. Zoom leaves a set size alone. |

The terminal takes `fontWeight` and `fontFeatures` too. The editor lines up columns, indent guides and the caret in
cells as wide as the font's `0`, so a font whose letters differ in width lines them up only roughly.

```json
{
  "fontFamily": "JetBrains Mono, monospace",
  "fontSize": 14,
  "fontFeatures": ["calt=0"],
  "lineHeight": 1.6,
  "terminalFontSize": 12
}
```

## Keyboard shortcuts

**File → Open Keyboard Shortcuts** opens `keybindings.json`, beside `settings.json`, in VS Code's format, so a VS Code
or Cursor file can be pasted in. Saving it applies it in every window at once. A file that does not parse is left alone
until it does, and a key or a condition Mayoi can't read is named in a toast.

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

Some commands are only in the command palette: **Copy Path of Active File**, **Copy Relative Path of Active File**,
**Reveal Active File in Finder** (File Explorer on Windows), **Indent Using Tabs** and **Indent Using 2, 4 or 8 Spaces**. **Selection → Sort Lines
Ascending**, **Sort Lines Descending** and **Transform to Camel Case** work on the lines selected, or every line, and on
the words selected, or the caret's.

### Windows and Linux

The shortcuts are macOS's with Ctrl for ⌘, and the hints show them so, but for those VS Code has others for:

| Command                                  | macOS     | Windows and Linux |
| ---------------------------------------- | --------- | ----------------- |
| Find Next, Find Previous                 | ⌘G, ⇧⌘G   | F3, Shift+F3      |
| Replace                                  | ⌥⌘F       | Ctrl+H            |
| Show the tab 1 to 8, the last            | ⌘1 to ⌘9  | Alt+1 to Alt+9    |
| Save All, Open Folder in New Window      | ⌥⌘S, ⌥⌘O  | none              |
| Focus Next Column, Focus Previous Column | ⌥⌘→, ⌥⌘←  | none              |

Ctrl with Alt is AltGr there, which types text, so no shortcut takes it: bind those commands in `keybindings.json`,
as `{ "key": "ctrl+k s", "command": "workbench.action.files.saveAll" }`. A file can tell the systems apart with
`isWindows`, `isLinux` and `isMac`.

## Updates

Release builds check GitHub Releases for a new version once a day, 10 seconds after launch when a check is due, and
**Mayoi → Check for Updates…** (**Help → Check for Updates…** on Linux and Windows) checks right away. A window shows the
release notes and offers to install the update, skip that version or remind you later. Updates download only what
changed when they can, are checked against Mayoi's signing key, and apply when Mayoi relaunches. Development builds, and
copies Mayoi can't write to, never check.
