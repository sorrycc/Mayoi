# Architecture

Mayoi is a single Go package (`main`) with three internal packages, drawn by [MyGo](https://github.com/egoist/mygo).
Chroma highlights code on a background goroutine, libghostty-vt runs the terminal, and fsnotify keeps the file tree and
the quick-open index current. Undo history, editing commands and decorations belong to the app rather than the text
area, so every column showing a file shares its text and history. This page walks through the source by area.

## Windows and commands

**`main.go`, `ipc.go`, `internal/cli`.** These manage the windows. Folders and files reach the app in three ways. The
`mayoi` command is the app's own executable, which on macOS the installed script runs with `MAYOI_CLI` set, and which
on Linux is the command itself: it reads its options (`cli.Parse`), and sends them as JSON over a Unix socket in the
data folder, which the running app listens on, to `serve` on the main thread. The reply is what to print, or what went
wrong, and with `--wait` it comes once the files are closed. With no app running, the command starts it, and sends what
it asked once the app answers; folders and files alone go to the app as it starts, as the Finder's do. The Finder hands
the app folders and files to open, and a second Mayoi started with paths passes them on and exits. Each folder goes to
the window that already has it, or to a new one. A file goes to the window whose folder contains it, and a command to
the project whose folder contains the directory it ran in.

**`projects.go`.** A `frame` is a native window, and a `desktop` a project in it, with its own `app`. A window
holds one project, or with project tabs every one, which a rail on its left chooses among. Only the project shown is
built. The others run what is posted to them as the window draws, and as one goes out of sight its editors note where
they were scrolled, as a hidden tab's do, since their text areas are built anew when it shows again. Each project's
elements are keyed by its window's ID, so that its tabs, numbered as another's are, never take another's state.
Turning project tabs on moves the apps into the window in front, and off gives each a window of its own again.

**`app.go`, `commands.go`.** These hold state and commands. Each project has its own `app`, and the windows share the
settings and the menu bar. A menu item runs the command of the window it was chosen in. Menus, accelerators and tests
all run the same commands. The menu bar is built anew when a window comes to the front or the recent folders change, so
that **Open Recent** leaves out the front window's folder.

**`internal/settings`.** The settings are two JSON files that the app writes a moment after each change: the preferences
a person chooses in `settings.json`, and the windows, tabs, recent folders and layout in `state.json` beside it. It
watches the folder too, and when the file changes into something it did not write, it reads the preferences from it
(`Prefs`, which the settings embed) and `main.go` applies them on the main thread, in every window. The windows and the
layout stay as the app has them. Each frame, a window checks the code's font and the terminals' against the settings and
measures them anew when they differ.

**`locales`.** What Mayoi says is in catalogs of ICU messages, `en.json`, `zh-Hans.json` and `ja.json`, from which
`mygo generate` writes `catalog_gen.go`, a typed function per message, in the language the app shows. The views say
their words each frame, so they follow a change of language as they draw. What is made once, the menus and the command
palette's titles, is made again when `App.OnLocaleChanged` tells of it. The `locale` setting, which **View → Language**
writes, goes to `App.SetLocale`. The commands are labelled by `commandsIn`, in a language given, so that the palette
matches their English titles too. The text sent to agents and Git's own messages stay as they are.

**`internal/keymap`, `keymap.go`.** `keybindings.json` is read in VS Code's format, comments and all, and watched as the
settings are. Every place that takes keys itself, the editor, the terminal (`OnKey`), the Explorer, diffs, pictures and
text fields, asks the keymap first with `takeKeys`, as the key comes, so that a chord's next key never types. Where the
keyboard is in none of them, the window registers the keys of the file as its shortcuts. A binding's command runs with
the next frame, by Mayoi's id or VS Code's name, and `runCommands` steps through its commands one frame at a time, until
the edits and Git commands before each are done.

**`view.go`.** This is the layout. The sidebar, the columns and the panel keep stable keys as panes come and go, so
editors never lose their state. Tabs are dragged as values within the window, and the drop targets a column offers only
show during a drag.

## Documents, editors and columns

**`document.go`, `columns.go`.** A document is a file's text, its undo history and its Git marks. An editor is a
document as one tab shows it, with its own selection, scroll position and paint caches. A column holds tabs, each of
which is an editor or a terminal.

**`columnlock.go`.** A locked column stays when empty and does not take implicit
file opens or new terminal tabs. `fileColumn` selects the current unlocked column
or the last one used, creating a workspace on the left if all columns are locked.
Explicit drops and session restoration name their target column directly. Removing an empty file
column keeps the last unlocked workspace and transfers width only to other
unlocked columns, so locked columns do not expand as files close. Sessions keep
each column's lock and the last file column along with the existing width weights.

A column has at most one preview tab, marked `transient`. The Explorer and search open files with `previewFile`,
which shows a file in the tab that has it, else in the preview tab's place, dropping the document it showed unless
another tab does, else in a new one. Opening a file the usual way, an edit, a double click on the tab and a drag to
another column keep the tab, and only kept tabs go among quick open's recent files. A commit clicked in source control opens with
`previewCommit` in the same place, through `previewIn`, which both share; `showCommit` keeps it.

Typing is credited to the editor that has the keyboard, and the selections of the document's other editors move with
the text. A text area scrolls to its caret when its text changes. So an editor without the keyboard parks the area's
caret in view, keeps its own selection, and stays where it was scrolled.

**`editor.go`, `wrap.go`.** The editor is a `TextAreaBufferBase` inside a container that scrolls sideways.

- **Unwrapped,** every line has the same height. Line numbers are painted into the area's left padding at paint time, so
  they stay in step while scrolling.
- **Wrapped,** the area is as wide as the view and reports where it laid out each line, using MyGo's `TrackLines`.
  Decorations are placed along a line as if it did not wrap, then split onto its visual rows.

Only the lines in view get `TextRanges` colors. Those colors are kept from frame to frame, and merged into as few ranges
as possible.

## Editing and undo

**`history.go`.** Undo and redo belong to the app, not to the text area. The buffer is a persistent tree, so each step
keeps the text from before an edit at little cost. Typing done by the text area is detected when its frame starts, and
joins the previous step until a pause or a caret move.

**`editing.go`, `autoclose.go`.** Editing commands are functions of a document and its selection. Keys reach them
before the text area does, and they run in the next frame, once the selection is known. Brackets close by following up
the edit that typed them, within the same undo step.

**`vim.go`.** In vim mode, the keys of normal and visual mode reach `editorKey` as key presses and are read as a
command there. The mode the command leaves changes at once, so the keys after it are read the right way. The command
itself runs in the next frame, as the other edits do. Typed text goes to the text area's input method rather than to
`HandleInput`, so normal mode makes the area read-only, which also hides its caret, and paints a block for the cursor.
An insert started by a change joins that change's step of undo: `history.group` keeps the step open until Esc.

## Decorations

**`decorations.go`, `selection.go`, `sticky.go`.** These paint what goes around the text: indent guides, bracket pairs,
other occurrences of a word, and the selection with its whitespace.

- Each decoration is computed for the visible lines only, and recomputed only when the text, the caret or the view
  changes.
- Decorations are placed by measuring the text the way the text area lays it out, with tabs at the platform's stops.
- Sticky lines and breadcrumbs are derived from indentation, so they work in any language.

## Image viewer

**`viewer.go`.** An image tab holds a viewer instead of a document. The bitmap is decoded off the main thread and
painted over a checkerboard, at a scale and offset the viewer keeps.

An SVG preview is a viewer shown in place of its editor. It parses the text shortly after each edit and keeps the last
image that parsed. The text area stays built, out of sight, so the editor keeps its place.

## Git

**`internal/git`.** This package runs the `git` command, never a library. It finds the work tree with `git rev-parse`
and keeps the root spelled as the folder it came from, so paths match the editor's even through a symlink. It reads
`git status --porcelain=v2` and runs stage, unstage, discard, commit, blame, switch, fetch, pull and push. Commands run
with `GIT_TERMINAL_PROMPT=0` and SSH in batch mode, so a login that needs a prompt fails instead of hanging.

**`git.go`, `scm.go`.** The window keeps the status, read in the background when the tree sees a change, after a save
or a Git command, when the window gets focus, and every 5 seconds. A status that reads the same as the last is dropped,
so what is worked out from it is kept: the groups of source control and their rows are made once for each status, and
the diff lists its files again only for a new one. The last commits are read again when HEAD or its upstream moves. Every 1.5 seconds it also checks the times of the
index, `HEAD` and `FETCH_HEAD`. A commit or checkout made in a terminal changes them, and then the documents compare
their lines again. Commands from the source control view run in the background, and the status is read again after
each.

**`internal/gitmarks`, `internal/linediff`.** The text at `HEAD` is read with `git show`, and the lines are compared using
Myers' diff in the background after each edit. Comparing a few edits in a 100,000-line file takes about 0.25 ms. The
diff gives hunks, runs of lines replaced, which the gutter marks, the peek box and the diff tab all use.

**`hunks.go`.** Reverting a hunk is an edit of the document. Staging one cannot apply the HEAD hunk to the index
directly, because the index may already differ from HEAD. So the index text is compared with the editor's again, the
hunks of that comparison that overlap the change are applied, and the result goes in with `git hash-object` and
`git update-index`. There is no patch to build and no fuzz to go wrong.

**`diffview.go`.** A diff tab paints its rows itself in a pane that scrolls both ways, rather than using text areas. A
text area has no way to leave the blank rows that side by side needs across from lines the other side lacks. Only the
rows in view are painted, each side has its own highlighter, and word changes are found for the rows in view. The
sides are read-only but for the working tree's, which the diff edits in
place: its typing, its undo and its save state are the file document's
one history, shared with the file's editor (`diffedit.go`), and the other
sides stay read-only. Double-clicking opens the file to edit.

The one diff tab holds every file the status lists, staged and not, each read and compared on its own, four at a
time, and only those within a view's height of what shows. `diffrows.go` lays the rows out again only when what they
show changes: a file read, a gap opened, the layout, or the width while lines wrap. As a file above the view is read
and grows, the row chosen, or else the row at the top, keeps its place on screen. It folds the unchanged lines into gaps, keyed by the line of the new side they start at so an
open gap stays open as the file is typed in, and puts a header before each run of changes. A row may be several lines
tall as its line wraps, so each row's top is kept, and the row at a point is found by binary search.
`diffpaint.go` paints the rows in view.

`diffhunks.go` stages, unstages and discards a run of changes from the diff as `hunks.go` stages one from the gutter:
it reads the index and the file again, finds the changes again among theirs, and puts the result in with `git
hash-object`. Discarding edits an open document, or else writes the file.

A commit's tab is the same view in another mode. It lists the files the commit changed from its first parent with
`git diff-tree`, reads each side with `git show`, and puts a card of the commit's message before them. Its files never
change, so it reads nothing again as the work tree does.

**`search.go`.** Search in files reads the files of the quick-open index on a few goroutines, and an open document's
text when it has unsaved edits. Each search has a number, and what an older one posts back is dropped, so typing never
shows stale results. Found files come back every 60 ms and are kept sorted by path. A document edited, or a file the
tree reports changed, is searched again alone and its results replaced. The folders of results are watched so a file
changed on disk is noticed even when its folder is closed in the tree. Replacing finds the matches again in the text as
it is, then edits an open document as one step of undo or writes a closed file in the background.
`internal/workspace/glob.go` matches the files to include and to exclude.

**`branchpicker.go`.** Switch Branch is a box like quick open's. It reads the branches, local and of the remotes, with
`git for-each-ref` as it opens, the one committed to last first, and finds them with the same fuzzy matcher as files.

**`recentpicker.go`.** Open Recent's box is the same kind of box over the recent folders the settings keep. ⌃R is a
shortcut of the view rather than of the menu, so that a terminal with the keyboard keeps it.

**`blame.go`.** Once the caret rests on a line for 350 ms, `git blame --contents -` blames that line of the editor's
text, unsaved edits included.

## Internal packages

**`internal/highlight`.** Chroma lexing runs on a background goroutine. It lexes the whole file up to 6,000 lines, and
a window around the view beyond that. A file just opened has the lines around the view lexed first.

- **After an edit,** only the lines around it are lexed again. Lexing starts a little above the edit, at a line outside
  any string or comment over lines, and is kept only if the lines before the edit come out as they did. Below the edit
  it stops once a few lines come out as they did. A keystroke in a 6,000-line Go file costs about 0.7 ms rather than
  170 ms.
- **Once typing pauses,** the whole range is lexed again and any line that came out otherwise is put right, for the
  lexers whose state the tokens around a line do not tell.
- **Until a pass is done,** the lines an edit moved keep their colors, and so does a line edited by one insertion or
  deletion. A line changed otherwise shows plain, so stale colors never land on the wrong text.

**`internal/workspace`.** Directories are read as they are expanded and watched with fsnotify, with bursts of changes
batched together. This package also holds the file index and the fuzzy matcher for quick open. A remote tree has no
fsnotify: it runs `inotifywait` on its host when there is one, and otherwise reads the folders the Explorer shows, and
stats the files open, every 3 seconds, at one round trip, reporting only what differs from what it saw last.

## Remote folders

**`internal/remote`.** A remote file has a path of its own, `/ssh:host/path`, which `filepath` takes as any other, so
documents, the tree, Git and the settings keep it as they keep a local one. The connection to a host is an ssh master
(`ControlMaster`) that every command goes through, with `BatchMode` so that it never asks for a password. A command is a
small POSIX shell script: `sh` on the host reads its length and then the script from what ssh sends, so that nothing of
it goes through the login shell, which may be fish, and the rest of the input is the script's, as a file's text. It
reads a folder with a loop of `[ -d ]`, stats with GNU's or BSD's `stat`, as the host has, reads files with `head -c`
and several at once as a tar stream, and writes a file whole to a temporary one first, then copies it in. `config.go`
lists the hosts of `~/.ssh/config`.

**`internal/vfs`.** The file system the editor uses: each function takes a local path or a remote one and does what
the `os` function of its name does. `Command` runs a program in a folder, on the machine of the folder, which is how
`internal/git`, `gitmarks` and the index run `git` and `find` on a host. A remote path takes a round trip, so the app
calls these off the main thread for one.

**`remote.go`, `remotesearch.go`.** Each project follows the connection of its folder's host. A remote document opens
at once, empty and read-only, and is read in the background; it saves in the background too, and records what stat
said of the file written, so that the change the watcher then reports costs a stat rather than the file again. While
the host is not connected, the documents are read-only and a banner offers to reconnect; reconnecting reads the tree,
the open files and the status again. Search in files asks `rg`, or else `grep`, on the host for the files that may
match, then reads those and matches them here, as it does local files, so that both find the same.

## Theme

**`theme.go`.** The palettes for the window, the code and the terminal: Mayoi's own light and
dark, and the other themes that the `theme` setting names: The Matrix, which makes the native window dark whatever the
`appearance` setting is, and Claude, Qoder and Grok Bot, which have a light and a dark palette and are as the appearance is. `rain.go` paints The Matrix's digital rain behind the welcome page,
worked out from the time alone, a step every 70 ms. `mascot.go` places a theme's character on the welcome page and in
the status bar, its pose also worked out from the time, asking for a frame only when the pose next changes: `clawd.go`
paints Claude's from cells, `qoduck.go` Qoder's duck from vector shapes, and `grokbot.go` Grok Bot's face from them too. It also styles what floats over
the window, dialogs and the boxes over an editor, and fades them in as they show.

## Performance

These figures come from the tests, run on an M-series Mac:

- A 100,000-line file opens in about 20 ms.
- Scrolling and typing cost under 2 ms per frame, or about 3 ms with find open.
- Highlighting again after a keystroke in a 6,000-line file takes under 1 ms.
- Quick open searches 100,000 paths in about 1.5 ms.
