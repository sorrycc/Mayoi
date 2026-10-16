# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## Unreleased

- The Explorer and the Source Control tree indent each folder level less,
  as VS Code does, so deeply nested files keep room for their names.
- Linux: Mayoi no longer sometimes crashes as it starts on machines that
  draw with the GPU (#24).
- Linux: text shows dark and crisp again where the desktop antialiases text
  by its subpixels, as on Ubuntu 22.04, where it was faint and tinted.
- Linux: tabs indent code to their tab stops; they took no room, so
  indented code lost its indentation and the caret stood off its text.
- Linux: a submenu of the menu bar, such as **View → Theme**, opens under
  the pointer rather than closing the menu.
- Shortcuts read as their keys on Windows and Linux, a cap for Ctrl, Shift
  and the key, on the welcome page and in Quick Open, rather than a cap
  for each letter.
- Linux: `install.sh` and the Debian package add the `mayoi` command, and
  `mayoi .` in a terminal gives the prompt back at once, leaving Mayoi
  open when the terminal closes.
- Opening a branch checked out in another worktree works with Git before
  2.36, as on Ubuntu 22.04.
- A paste to or from a remote host canceled as it finishes leaves nothing
  behind.
- A paste to or from a remote host canceled before it starts copies
  nothing; a small one could still arrive.
- Git work still running in a folder you switch away from no longer lands
  in the new folder's Source Control, where it cleared its busy message or
  loading state and ran its follow-ups.
- Source Control has a **Repository** section below the commits, closed at
  first, whose **Branches** and **Remotes** list each branch with its
  upstream and history, and each remote with its URLs and branches; their
  menus check out, pull, create, rename, delete, merge and rebase branches,
  and add, edit, fetch, prune and remove remotes.
- Selecting a file anywhere in its Explorer row switches to its editor,
  including clicks in the space beside its name, and moving through the
  Explorer with the arrow keys previews each file in turn.
- Review comments: choose lines in a diff (Shift-click, a drag or
  Shift+J/K) and press C, or select lines in a file and press ⇧⌘M, to
  comment on them. **Send Review Comments to Terminal** pastes them all
  into the terminal used last, for the agent there, each with its path,
  line numbers and lines; ⌘C in a diff copies the lines chosen so.
- A **+** button by the line under the pointer in a diff comments on it,
  or on the lines chosen with it, and a bar marks the lines of each
  comment, with a mark on the scroll bar.
- A diff with review comments offers **Send N Comments** in its toolbar,
  and Delete deletes the comment chosen in it.
- A file shows each review comment after the last of its lines; click it
  to edit the comment, or hover it to read it all.
- Review comment cards are redrawn: a stripe of the accent down their
  edge, an **Outdated** badge when their lines changed, and a Comment
  button that stays off until there is something to say.
- Review comments read in the window's font, wrapped to the card, on a
  raised card; side by side, each shows under the side of its lines,
  and an outdated one quotes the lines it was written on.
- Review comments are kept with the folder's session, so they are there
  again at the next start.
- `n` and `N` in a diff, and **Go to Next Review Comment** and **Go to
  Previous Review Comment**, go from comment to comment. The status bar's
  menu lists them under their files, and going to one on lines removed,
  staged or of a commit shows it in its diff.
- Deleting a review comment, or clearing them all, can be undone from
  its toast, and a comment being edited has a Delete button.
- A review comment after its lines in a file shows on a tinted chip.
- Lock a column from its tab menu to keep it for terminals: Move to Locked
  Right Column puts a running terminal there in one step. Files opened from
  the Explorer, Search, Quick Open or Source Control go to an unlocked
  column, which stays beside the terminal, at the same width, when its last
  file closes. An empty locked column offers a new terminal, ⌘W closes an
  extra empty column, and each project's session remembers the locks.
- Source Control selects several files: Shift-click or Shift+Up/Down
  for a range, Cmd-click (Ctrl-click on Windows and Linux) for one more,
  and Cmd+A for all. Space, and a selected row's stage, unstage and
  discard buttons and menu items, act on all the files selected.
- The Explorer, and Source Control as a tree, draw indent guides down
  from each open folder, in the Explorer brighter for the folder of the
  file shown, and a shadow under the folder's name as the files scroll
  under it.
- The Markdown preview draws a task's checkbox as a box, filled once
  done, and lists with softer bullets, rings for a list in a list.
- A picture fitted to its tab leaves a margin around it, and a hairline
  sets it off from the pane.
- The editor does less work on every frame it paints: idle and scrolling
  frames of a large file allocate about a quarter of what they did.
- Windows: the programs Mayoi runs in the background, git above all, no
  longer open a console window of their own, which reached the desktop as
  a terminal window that flashed open and closed around every command the
  editor ran.
- MyGo 0.5.5: a double click is counted by the system's double-click
  settings, and programs in the terminal that read the clipboard can take
  pasted images. Mayoi still builds on a fork of it, with one change to
  draw long files with less work until it is released upstream.
- A commit's menu saves or copies its patch, binary changes included,
  compares it with HEAD or with a commit chosen before, and opens it on
  GitHub, GitLab or Bitbucket.
- **File → New Window** (⇧⌘N) opens a window that shows nothing, which
  the welcome page fills, and on macOS the Dock icon's menu has it above
  the recent folders.
- macOS: files and folders dropped on the Dock icon open, as `mayoi
  <path>` opens them, and Mayoi is in the Finder's **Open With** menu
  for any file (#28).
- The middle mouse button closes the tab under the pointer, without
  showing it first, as a browser's tabs do: the file you are reading
  stays where it is. Let go of it off the tab, and the tab stays open.

## 0.9.1

- ⇧⌘E gives the Explorer the keyboard, as in VS Code, so that the arrows
  choose a file and ⌘V pastes into the folder chosen.
- Remote SSH: files copied in the Finder paste into a folder of the host,
  and files dropped from the Finder on the explorer copy into the folder
  under the pointer. A remote file copied in the explorer pastes as a
  copy on its host, or downloads into a folder of this machine.
  **Upload…** and **Download…** in the explorer's menu do the same by a
  dialog. The status bar shows each transfer and how far it got, and a
  click on it cancels one.
- Remote SSH: ports of the host forward to this machine. A link to
  `localhost` clicked in a terminal of the host forwards its port and
  opens it in the browser, and **Forward a Port…** forwards one by its
  number. The status bar shows the ports forwarded, with a menu to open,
  copy or stop each.
- Remote SSH: the folders of the Explorer open at once: those in a
  folder are read along with it, at one round trip to the host.
- Remote SSH: saving a file that changed on the host since it was
  opened asks whether to overwrite it or revert to it, rather than
  writing over the change.
- Remote SSH: a terminal of the host no longer closes as the connection
  goes: it keeps what it showed, says the connection was lost, and
  starts again as it comes back.
- Remote SSH: a terminal of the host pastes files, and a screenshot alone
  on the clipboard, as copies uploaded to the host, and types their paths
  there, so Claude Code on the host takes a pasted screenshot. A remote
  file dragged to a terminal types its path on the host, not Mayoi's.
- A folder opened again just after `git init` shows its Source Control:
  the look for its repository made before no longer has the last word.
- With Git 2.55 or later, the diff of a submodule shows the commits it
  went from and to again, not the log of the submodule's commit.
- Remote SSH: a connection that drops, as the network goes or the laptop
  sleeps, connects again by itself, after 2 seconds, then less often, up
  to every 30 seconds, and at once as the window comes back to the front.
  The banner says when it tries next, with **Reconnect Now**; a host that
  refuses the keys, or whose key is not trusted, says so and waits.
- Remote SSH: the Connect to Host box groups the recent hosts, with the
  folder opened last on each, apart from those of `~/.ssh/config`, and
  marks the letters the query matched. While it connects it says as whom
  and for how long, and Escape goes back to the hosts.
- Remote SSH: the folders of the host list by name, with `..` to go up,
  `~` for the home folder, and why a folder could not be read; the host
  shows beside the path.
- Remote SSH: the Explorer of a folder whose host is not connected says
  so, with a button to reconnect, and the title bar names the host.
- Remote SSH: a remote folder looks for changes every 15 seconds rather
  than 3 while its window is in the background, and at once as it comes
  back; who each host signs in as is remembered until `~/.ssh/config`
  changes, and a remote file opens at one round trip to the host, not
  two.
- **View → Project Icons** (or `"projectIcons": true` in `settings.json`)
  shows each project's favicon or logo on the rail, from its well-known
  icon files, `favicon.ico` included, or the icon its sources declare,
  rather than its initials. A project without one keeps its initials.
- Project Icons also finds a mygo app's icon (the one `mygo.json` names,
  or `resources/icon.png`), the favicon a `site/` or `docs/` page
  declares beside it, and a macOS app's `.icns` or Xcode AppIcon.

## 0.9.0

- Remote SSH: **File → Remote SSH → Connect to Host…** opens a folder on
  another machine with the system's `ssh`, with nothing to install there.
  It lists the hosts of `~/.ssh/config` and those connected to before,
  then the folders of the host. Files open and save in the background,
  and the Explorer, quick open, search, Source Control and terminals run
  on the host, which `inotifywait` or a poll every 3 seconds watches for
  changes. Disconnecting keeps the window, read-only, until it connects
  again. Keys and the SSH agent sign in; Windows does not have it yet.
- A selection made from right to left grows from where it started:
  ⌘⇧← or Shift-Home takes it to the start of the line, where it used to
  drop the part already selected, and the caret's line, the blame beside
  it and the scrolling follow the caret, not the selection's far end.

## 0.8.2

- Selecting text in a terminal copies it to the clipboard, as in Ghostty
  and iTerm2: a drag, a double or triple click, a Shift-drag in a
  program that takes the mouse, or Select All. Set
  `terminalCopyOnSelect` to `false` in settings.json to turn it off.
- Each project keeps its own sidebar and panel: whether the sidebar
  shows the Explorer, Search or Source Control, whether it is open, how
  wide it is, and how tall the terminal panel is. Switching project tabs,
  or windows, no longer carries one project's layout to the others. A
  project opened anew starts with the layout of the one in front, and a
  folder opened again gets back the layout it had.
- ⌘V pastes a screenshot into Claude Code in the terminal, as in VS Code:
  with only an image on the clipboard, the terminal sends an empty
  bracketed paste, which Claude Code takes as its cue to read the image
  itself. Mayoi builds on a fork of MyGo with this fix until it is
  released upstream.
- A new file in a folder whose name starts with a dot, like
  `.changeset`, shows its lines in Changes instead of a git error, and
  its hunks can be staged. A file's error in Changes no longer runs
  over its header.

## 0.8.1

- The rail of project tabs is redrawn: a pill along its edge grows from
  the project under the pointer to the one shown, the squares ease
  between their states, and a dot cut out of a square's corner marks
  unsaved changes. A square's tip, to its right, gives the project's
  name, its folder and its keys. Drag a square along the rail to reorder
  it, or drop a folder from the Finder on the rail to open it. The button
  after the squares opens another folder, and a square's menu adds
  **Reveal in Finder**, **Copy Path** and **Close Other Projects**.
- With project tabs, a terminal that rings its bell or sends a
  notification in a project out of sight, as a build or an agent does
  as it finishes, marks the project's square in the accent until the
  project shows, and its tip says why.
- The explorer of an empty folder says so, with **New File…** and **New
  Folder…** buttons.

## 0.8.0

- The title bar and status bar now adapt to narrow windows: the status
  bar leaves out its least needed parts, the encoding first, only once
  they would not fit, and the command center leaves out the folder before
  it cuts the file's name short. The command center has a clearer control
  surface, active tabs are easier to identify, and Explorer rows have more
  comfortable spacing.
- Inactive editor tabs no longer lay out their text every frame, keeping
  rendering responsive as more files are opened while preserving each tab's
  selection and scroll position.
- Project tabs: **View → Project Tabs** (or `"projectTabs": true` in
  `settings.json`) keeps every folder open in one window, with a rail on
  its left of a square for each, its initials in it. A click shows a
  project, as do ⇧⌘] and ⇧⌘[ (**Go → Next Project** and **Previous
  Project**) and ⌃⌘1…9, each with its tabs, terminals and Explorer as it
  left them. Opening a folder adds it to the rail. **File → Close
  Project** closes one. Turning it on gathers the windows into the one in
  front, and off parts them into a window each again.
- The commits' context menu does what Sourcetree's does: **Merge into**
  the branch, **Rebase** it onto the commit, **Reset** it to the commit
  (Soft, Mixed or Hard), **Revert Commit** and **Cherry-Pick Commit**.
  When one stops for conflicts, source control says so with **Continue**
  and **Abort** buttons, which the menu and the command palette offer too,
  and the commit box takes the message Git proposes.
- The initials of a commit's author sit in the middle of their circle, in
  the history and on the commit's card.
- When Mayoi crashes, what ended it, with every goroutine's stack, is kept
  in `crash.log` in its data folder, and the last 10 crashes in its
  `crashes` folder, also when it was started from the Finder or the Dock.

## 0.7.0

- MyGo 0.4.8: shortcuts work on keyboard layouts that type no Latin
  letters, a selection paints above the text's highlights, and long files
  lay out and draw with less work.
- The Qoder theme: **View → Theme → Qoder** takes Qoder's Forest colors,
  light or dark as the Appearance is, with its code in Forest's greens,
  blues, teals and ambers. Qoduck, Qoder's round-faced duck, its beak
  tilted so that both eyes show, takes the logo's place on the welcome page
  and sits at the start of the status bar, blinking now and then and
  hopping while Git or a search works, as Clawd does in Claude.
- Switching to a branch checked out in another worktree offers to open
  that worktree, in this window or a new one; when its folder was deleted,
  to prune it and switch.
- Better on Windows and Linux: the menus' shortcuts work there, with VS
  Code's keys where Ctrl for ⌘ would collide or be AltGr (F3 and
  Shift+F3 find the next and previous match, Ctrl+H replaces, Alt+1 to
  Alt+9 show a tab), the terminal leaves the shell its Ctrl keys but the
  workbench's, vim's normal mode leaves ⌃F, ⌃B and ⌃R to Find, the
  sidebar and Open Recent unless `vimCtrlKeys` is on, tooltips and hints
  show Ctrl+… rather than ⌘, and the menus name File Explorer and the
  Recycle Bin.
- Keyboard shortcuts of your own: **File → Open Keyboard Shortcuts** opens
  `keybindings.json`, in VS Code's format, so those of VS Code or Cursor
  can be pasted in. It takes chords, as `ctrl+. g c`, the names of VS
  Code's commands, `when` conditions, `runCommands`, `vscode.open` and
  keys taken away with `-`, and applies as it is saved.
- New commands: **Sort Lines Ascending** and **Descending**, **Transform
  to Camel Case**, **Copy Path of Active File**, **Reveal Active File in
  Finder** and **Indent Using Tabs** or **Spaces**.
- The sidebar's title is no longer cut short: the Explorer's buttons show
  on the folder's row, source control's Refresh is in its **⋯** menu, and
  in a narrow sidebar the title reads **GIT** rather than **SOURCE C…**.
- Quick open's footer shows the prefixes it takes, **>** for commands,
  **:** for a line and **@** for a symbol, as keys, and its placeholder no
  longer repeats them.
- A button that loses something, as **Discard**, is a deeper red that its
  white label reads on in a dark window, and shades under the pointer
  as the other buttons do.
- **⇧⌘V**, or the button at the end of the breadcrumbs, previews a
  Markdown file as well as an SVG: headings, lists and task lists, quotes,
  code highlighted as in the editor, tables, cached local bitmap and SVG
  images, inline badges and links, with comments and front matter hidden, in
  the editor's fonts and colors. It follows your edits, and with the same
  file in another column shows its source beside it. An image or link
  starting with `/` is found from the repository's root, as on GitHub.
- A sideways swipe on a trackpad scrolls a long line smoothly, as in VS
  Code: it moves the text sideways only, rather than stopping every few
  frames and drifting up or down.

## 0.6.0

- **⌘C** in the Explorer copies the file or folder chosen, and **⌘V**
  pastes the files copied, in another window or in the Finder, into the
  folder chosen; a name taken gets "copy" added, as in the Finder. The
  menu of a file or folder has **Copy** and **Paste** too.
- Double-clicking a file in the Explorer keeps its tab open, and gives
  it the keyboard, rather than leaving it a preview.
- A commit clicked under **COMMITS** opens in the preview tab, its name
  in italics, and the keyboard stays in the list: clicking through
  commits leaves one tab. Enter, a double click or **Open Changes** keeps
  it.
- Drag a file from the explorer, from source control's changes or from
  the search results to open it where it is dropped: on a tab strip where
  the marker shows, on a column after the tab shown, or on the edge of a
  column in a new column there. A search match opens selected. Dropped on
  a terminal, the file types its path.
- A commit's tab wraps its message to the width of the view instead of
  cutting it at the right edge: the subject on up to three lines, the line
  of its author, date, hash and branches onto a second, and the body in the
  editor's font. A body longer than twelve lines folds under **Show More**.
  The message can be selected and copied, and a click on the hash copies
  it. A long subject in the breadcrumbs ends in an ellipsis.
- A click on the empty space below the files in the Explorer, or Escape,
  unselects the file or folder chosen, so new files and pasted ones go in
  the workspace's folder. A right click there opens a menu for the
  workspace: **New File…**, **New Folder…**, **Reveal in Finder**,
  **Open in Terminal** and **Paste**.

## 0.5.1

- **COMMITS** in source control draws the history as a graph, each branch
  in a lane and color of its own, and shows each commit's author, branches
  and tags; a branch and its remote's at the same commit share a badge.
  HEAD has a ring and a merge a hollow dot. The commit's tab shows its
  branches and tags too.
- **Show All Branches**, from the COMMITS header or the **⋯** menu, shows
  every branch and tag, not only the branch checked out. It is the
  `scmAllBranches` setting.
- A commit's menu checks it out, creates a branch or a tag there, and
  copies the name of its branches and tags.
- The **⋯** menu of source control no longer ignores an item chosen with
  the pointer past the sidebar, as **View as Tree** often was.
- A folder closed, by closing its window, **File → Close Folder** or
  opening another folder in its window, opens again with its tabs,
  columns and the folders open in the Explorer. Opening
  another folder in a window now closes the tabs of the one it had,
  rather than keeping them beside the new folder's.
- Tabs come back with their caret, selection, scroll position and own
  word wrap, and a diff showing every line still does, also when Mayoi
  starts again.
- The windows, tabs, recent folders and layout move from `settings.json`
  to `state.json` beside it, so `settings.json` holds only the
  preferences.
- Closing the last tab of a column with another after it no longer
  crashes Mayoi.

## 0.5.0

- **View → Theme → Claude**: Anthropic's ivory and warm near-black with
  Claude's orange. Clawd, Claude Code's little character, takes the logo's
  place on the welcome page and sits in the status bar, blinking now and
  then, and hops while Git or search works.
- **View → Theme** picks the theme (Mayoi, The Matrix or Claude) and,
  apart from it, System, Light or Dark, so Claude can be light or dark
  whatever the system is. They are the `theme` and `appearance` settings;
  a settings file with `"appearance": "matrix"` keeps The Matrix. Light
  and Dark are greyed out under The Matrix, which is always dark.
- **Search** in the sidebar (⇧⌘F) finds text in every file of the folder,
  with match case, whole word and regular expressions, and files to
  include and exclude. ⇧⌘H replaces across files, all at once, a file at
  a time or a match at a time, showing each replacement first. Open files
  change as a step of undo.
- Search's results: ↓ and ↑ walk every match, past the file headers, as
  far as the list scrolls; ↑ from the first match and Escape go back to
  the query. Each match shows its line number, matches stand out more,
  a bar moves while searching, and searches ignoring case are about
  twice as fast.
- **Preview tabs**: a file clicked in the Explorer or chosen in search's
  results opens in a tab with its name in italics, which the next file
  chosen replaces. Editing it, double-clicking it or its tab, or Enter
  keeps it. Moving through search's results with ↓ and ↑, or clicking
  one, shows each match in the editor while the keyboard stays in the
  results, and the match clicked becomes the one chosen.
- **File → Open Settings** (⌘,) opens `settings.json`, and saving it
  applies the fonts, theme, auto save, word wrap, vim mode and the other
  preferences in every window at once, rather than at the next start.
- New font settings: `fontWeight`, `fontFeatures` (as `["calt=0"]` to
  turn ligatures off), `lineHeight`, and `terminalFontFamily` and
  `terminalFontSize` for a terminal font of its own.
- Zooming the text resizes the terminals of every window, not only the
  window's own.
- **Reveal in Explorer** in a tab's menu shows the Explorer, chooses the
  file and scrolls to it, and the menu has **Reveal in Finder** too. The
  Explorer scrolls to the file shown as tabs change.
- The welcome page titles its sections as the sidebar titles its views.
- A diff says that a file has no changes in its text, or is binary, in
  the window's font rather than as a line of code.

## 0.4.0

- **Go → Command Palette…** (⇧⌘P), or `>` typed in quick open, runs any
  command by name. Each row shows its menu and its shortcut, and a check
  when it is on; the commands run last come first.
- The status bar no longer calls `go.mod` AMPL or every `.sql` file
  MySQL. Brewfiles highlight as Ruby and `.npmrc` as INI.
- In The Matrix, the welcome page sits on a panel over the digital rain,
  so its words read.
- Line numbers, comments and secondary text, such as the folders in
  quick open, read more easily in the light and dark themes.

## 0.3.3

- **View → Theme** (formerly Appearance) adds The Matrix: phosphor green on
  black, the whole window in the code's font, file icons in greens, and
  red and amber kept for what is removed, failed or changed. The terminal
  takes its colors, and digital rain falls behind the welcome page while
  the window has the keyboard.
- Clicking a file or a commit in source control focuses its changes, so
  `]` `[` go from change to change and `.` `,` from file to file at once.

## 0.3.2

- Drop files from the Finder to open them: on a column, on its tab strip
  where they should go, or on the side of a column to open them in a new
  column there. A folder dropped on a window opens as `mayoi <dir>` opens
  it. Dropped on a terminal, files type their paths, quoted for the shell.
- Primary buttons, such as **Commit**, stay purple under the pointer and
  as they are pressed, rather than turning blue. In a dark window they
  are a deeper purple, so their white labels read clearly.
- Quick open, the pickers and the prompts fade in from just above where
  they settle, and so do find and the peek of a change. Their shadows are
  lighter in a light window.
- The explorer of a window without a folder looks like source control's
  empty views, with a button to open one.

## 0.3.1

- Built with MyGo 0.4.1: a select or combobox list no longer jumps back
  to its highlighted option while you scroll it, and text an input method
  is composing keeps its highlighting.
- The disk images and Windows installers of a release are named
  `Mayoi.<version>.<arch>.dmg` and `Mayoi.Setup.<version>.<arch>.exe`.

## 0.3.0

- **View → Vim Mode** edits with vim's keys: normal, insert and visual
  modes, motions, operators and text objects with counts, `.` to repeat,
  and yanks and pastes through the system clipboard. The status bar shows
  the mode, and normal mode shows a block cursor.
- **View → Sidebar on Right** moves the sidebar, and its button in the
  title bar, to the right of the editors.

## 0.2.1

- Source control lists the last commits of the branch, with those not yet
  pushed marked. Click one to see its message and its changes in a tab of
  its own; its menu copies the hash or the message.
- The commit button says what it will do (**Commit**, **Commit All**,
  **Amend Commit**), and its menu has Commit & Push, Amend and the new
  **Undo Last Commit**, which keeps the changes staged and the message.
- **Switch Branch…** finds a branch by name among the local ones and the
  remotes', the one committed to last first, with its last commit, and
  creates a branch of the name typed. The branch at the top of source
  control opens it, and the pull and push buttons beside it show how many
  commits they would move. A branch with no upstream gets **Publish
  Branch**.
- **View as Tree**, in the **⋯** menu of source control, lists the changes
  under their folders, which stage, unstage or discard all they hold.
- A clean work tree says so, and offers to push or pull when the branch is
  ahead or behind. A folder outside a repository explains itself.
- The summary line of a commit message shows its length past 50
  characters, in a warning color past 72.
- A thin bar under the title moves while a Git command runs, and a toast
  says when a commit, push or pull is done. The commit's toast has an
  **Undo** button.
- Stage, unstage or discard one change at a time from the Changes tab:
  point at a run of changes for its buttons, or press `s` on a line.
- A file's header stays at the top of the diff as you scroll through it.
- Diff headers show the file's icon, a badge of its state, where a renamed
  file came from, and a bar of the lines added and removed. Folded lines
  read "Show N unchanged lines" and light up under the pointer.
- Source control and the Changes tab stay quick with thousands of changed
  files: an idle frame of 2,000 changes went from 0.39 ms to 0.22 ms and
  allocates a tenth as much, and reading the status again when nothing
  changed no longer lays the diff out again.
- **File → Open Recent** lists the folders opened last, but the window's
  and those not on the disk, with **Clear Recently Opened**. ⌃R, or
  **More…**, finds one by name: ⏎ opens it in the window, ⌘⏎ in a new
  one. A terminal keeps ⌃R for its shell. On macOS the Dock icon's menu
  lists them too.

## 0.2.0

- Source control: the sidebar lists the files changed, staged and not, to
  stage, unstage, discard and commit, with Commit & Push and Amend.
- Diffs open in a tab, in one column or side by side, with the words that
  changed tinted, and follow the file as you type.
- Diffs fold the unchanged lines away from the changes, head each change
  with its `@@` line and the block it is in, wrap long lines, and go side
  by side when there is room.
- Every file changed shows in one **Changes** tab, staged and not, each
  under a header that folds it, stages it or discards it. Clicking a file
  in source control scrolls the tab to it.
- In a diff, `j`/`k`, `]`/`[`, `.`/`,`, Space, `g`/`G` and `z` move about as
  in a pager.
- Click a gutter mark to see what the lines were, and revert or stage that
  change alone. ⌥F5 goes to the next change.
- The Explorer shows each changed file's letter and color.
- The branch in the status bar switches and creates branches, says how far
  it is ahead and behind, and fetches, pulls and pushes.
- The caret's line shows who changed it last.
- ⇧⌘T (**File → Reopen Closed Tab**) reopens the last tab closed, where it
  was and scrolled as it was. Terminals come back as a new shell.
- Highlighting keeps up with typing: after a keystroke only the lines around
  the edit are lexed again, under 1 ms in a 6,000-line file where the whole
  file took 170 ms, and lines an edit moves keep their colors meanwhile.
- A large file opened shows the colors of the lines in view first.
- The tab strip has an edge along its bottom, which the tab shown breaks.
- Line numbers are easier to read, the find and replace fields line up, and
  the status bar says "1 line", names languages consistently, and shows a
  terminal's folder and size.

## 0.1.0

- The first release: tabs, columns, the file tree, quick open, find and
  replace, Git marks, a terminal, an image viewer and auto save.
- Mayoi updates itself from GitHub Releases, from **Mayoi → Check for
  Updates…** or in the background once a day.
