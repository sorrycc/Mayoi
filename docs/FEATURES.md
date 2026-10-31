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

The middle mouse button closes the tab under the pointer, wherever in the tab it is pressed, without showing it first:
clearing tabs leaves the file you are reading where it is.

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
- Cut (⌘X) cuts the selection, or the entire line at the caret when nothing is selected. Undo restores it in one step.
- Home goes to the first non-blank character of the line.
- The status bar shows the file's indentation and line endings, and you can change either from there.

### Vim mode

**View → Vim Mode** edits with vim's keys. Each tab has its own mode, shown in the status bar, and starts in normal
mode, where the caret is a block.

- **Motions:** `h j k l`, `w b e ge` and `W B E gE`, `0 ^ $ g_`, `gg G`, `H M L`, `f F t T` with `;` and `,`, `%`,
  `{ }`, `[( [{ ]) ]}`, Enter and `-`. Arrows, Home and End move too. A count goes before any of them.
- **Operators:** `d c y`, `> <`, `gc` to comment, and `g~ gu gU` for case. They take a motion or a text object (`iw
  aw iW aW`, `i" a"` and the other quotes, `i( a(`, `i[`, `i{`, `i<`, `it at` for tags, `ip ap`), or apply to lines
  when typed twice,
  as `dd`.
- **Commands:** `x X D C Y s S r J gJ ~ p P gp gP o O`, `i a I A gi` to insert, as many times as a count before them,
  as `3ix`, `u` and ⌃R to undo and redo, `.` to repeat
  the last change, ⌃A and ⌃X to add to a number and take from it, ⌃D ⌃U ⌃F ⌃B to scroll by half a view or a view, ⌃E
  ⌃Y by a line, and `zz zt zb` to put the cursor's line in the middle, at the top or at the bottom.
- **Registers:** `"a` to `"z` before a command yank and delete into a register of their own, and paste from it; `"A`
  appends to it. `"0` holds the last yank, `"1` to `"9` the last deletes of lines, `"-` the last small delete, `"_`
  keeps nothing, `".` the text inserted last, `":` the last command and `"/` the last search. `:reg` shows them.
- **Macros:** `qa` records the keys typed into register `a`, with the status bar saying so, until `q`. `@a` plays them,
  `3@a` three times, `@@` the register played last, and `@:` the last command. A macro stops where a motion fails, as
  in vim.
- **Marks and jumps:** `ma` sets a mark, `'a` goes to its line and `` `a `` to it, and the marks move with the text as
  it changes. `''` goes back to where the cursor jumped from. Jumps, as `G`, `%`, a search or a mark, are kept: ⌃O goes
  back through them and ⌃I (Tab) forward again. `:marks` lists the marks.
- **Visual mode:** `v` selects characters and `V` lines, and `gv` the last selection again. A selection made with the
  mouse is one too. Motions extend it, `o` swaps its ends, and operators, `r`, `J`, `~ u U` and `p` act on it.
- **Search:** `/` and `?` open a line over the bottom of the editor. The cursor goes to the first match as you type,
  every match is highlighted, and the line counts them. Return searches, Esc goes back, and ↑ ↓ go through the
  searches before. `n N` search again, `* #` search for the word under the cursor, and a search is a motion too, as in
  `d/end`. Patterns are vim's, with `\( \) \| \+ \< \>`, or `\v` for the rest; case is ignored unless the pattern has a
  capital, or `\C`. The matches show until Esc in normal mode, or `:noh`.
- **Commands after `:`:** `:w`, `:q`, `:wq`, `:x`, `:wa`, `:qa`, a line number as `:42`, `:s/old/new/g` with `%`, a
  range as `2,10` or that of the selection, `& \1 \n` in the replacement, `:d`, `:y`, `:j`, `:> :<`, `:sort` (with
  `!`, `i`, `n` and `u`), `:m` and `:t` to move and copy lines, `:pu` to put a register, `:normal` to type keys on
  every line of a range, `:g/pattern/command` and `:v` for the lines that match or do not, `&` and `g&` to repeat the
  last `:s`, `:noh`, `:e file`, `:vs` and `:bn :bp`. A mark's line, as `'a`, is an address too. `ZZ` saves and closes,
  and `ZQ` closes. `:g`, `:normal` over lines and `:%s` undo as one step.

Esc, ⌃[ or ⌃C leaves insert mode. A change and the text typed after it undo as one step. Yanking, deleting and pasting
use the system clipboard unless a register is named, and a yank of whole lines pastes as lines. Where Control is not
the key of shortcuts, ⌃W and ⌃U delete the word and the line before the caret in insert mode. In normal mode, ⌃R redoes rather than opening
recent folders. ⌘ shortcuts work as usual in every mode. Punctuation keys are read as a US keyboard types them. On
Windows and Linux, ⌃F, ⌃B, ⌃R and ⌃O stay Find, Toggle Sidebar, Open Recent and Open in normal mode, and ⌃A and ⌃X
Select All and Cut, unless `vimCtrlKeys` is on.

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
- A folder in the breadcrumbs, clicked, shows its files in a tree below it, apart from the Explorer's: open its
  folders, and choose a file to show it in that column, with the mouse or the arrows and Enter.

### Git

The gutter marks lines added, changed or removed since the last commit. A strip along the right edge shows these
changes, find matches and the caret position for the whole file.

Click a mark to see the lines as they were at HEAD, in a box under the change. From there you can revert the change,
which is a step of undo, or stage it, which puts only those lines in the index. ⌥F5 and ⇧⌥F5 go to the next and previous
change, and the **Git** menu has the same commands for the change at the caret.

The caret's line shows who changed it last, when, and the commit's summary, a moment after the caret stops. Turn this
off from **Git → Inline Blame**.

**Git → Open File on Remote** opens the file on GitHub, GitLab or Bitbucket at the upstream branch, or at the commit
checked out when the branch has none, marking the caret's line or the lines selected (`#L10-L20`). **Git → Copy
Permalink** copies the same link at the commit checked out. Both are in the tab's and the Explorer's file menus too,
which link the whole file. The remote is the upstream's, else origin, else the first.

## Source control

The button beside **EXPLORER** at the top of the sidebar (⌃⇧G) switches it to the changes of the folder's Git work
tree, with a count of them. Files are grouped as merge changes, staged changes, changes and untracked files.

- The row at the top has two buttons: the folder of this work tree, which opens **Switch Worktree…**, and the branch,
  which opens **Switch Branch…**. The folder names a linked worktree, and the name shortens as the sidebar narrows;
  the main one shows its icon alone. Beside the branch, fetch, pull and push, which show how many commits they would
  bring down or send up; a branch with no upstream gets **Publish Branch** instead. With an upstream, a row too narrow
  for its buttons, counts included, moves fetch, pull and push into the **⋯** menu; without one, fetch and publish stay
  in the row.
- **Switch Worktree…** lists the work tree's checkouts, the one open first, by folder and branch. Choosing one
  opens its folder in the window. Typing a new branch name offers **Create Worktree**, which makes the checkout beside
  the main one, in `<folder>.worktrees`, from the commit HEAD is at. Typing the name of a branch that is already there
  points to **Switch Branch…** instead. A folder that has gone missing is reported, not pruned.
- Click a file, or press Enter, to see its changes with the diff focused, so its keys (`]` `[`, `.` `,`) work at once.
  Shift-click or Shift+Up/Down selects a range of files, ⌘-click (Ctrl-click on Windows and Linux) adds or removes one,
  and ⌘A selects them all. Space, and a selected row's stage, unstage and discard buttons and menu items, act on all the
  files selected; on a row not selected they act on that file alone.
- The buttons on a row open the file, discard its changes, and stage or unstage it. A group's header does the same for
  all its files. Discarding asks first, and moves untracked files to the Trash.
- Write a message and press ⌘↵ or the commit button, which says what it will do: **Commit**, **Commit All** when nothing
  is staged (Mayoi asks before staging everything), or **Amend Commit**. Changed files save first. Past 50 characters
  the summary line shows its length, in a warning color past 72.
- Signed in to your [Mon](https://mon.sorrycc.dev) account, the ✨ button at the corner of the message box has AI write
  the message from the staged changes, or from every change when nothing is staged, in the style of the branch's last
  commits. The message streams into the box; click the button again to stop. Text already in the box goes along as
  notes to follow. **Git: Write Commit Message with AI** in the command palette does the same. It uses Mon's AI gateway
  and your account's daily limit; the `aiModel` setting picks another of its models.
- The arrow beside the commit button has **Commit & Push**, **Amend Last Commit** (which fills in the last message) and
  **Undo Last Commit**, which takes the commit back but keeps its changes staged and its message in the box.
- The **⋯** menu by the title has the commit commands, stage, unstage and discard all, fetch, pull and push, Refresh,
  and **View as Tree**, which lists each group's files under their folders. A folder with only one folder in it shows as
  one row, as `internal/git`, and a folder's buttons stage, unstage or discard every file in it.
- **Stash Changes…** in the **⋯** menu puts the changes away, with a message if you give one; **Stash Including
  Untracked…** takes the untracked files too. Open files save first, and the toast's **Pop** button brings the changes
  back. The **Stashes** submenu lists the last ten, each with **Apply**, **Pop** (apply, then delete it) and **Drop…**,
  which asks first. A pop whose changes conflict keeps the stash, and the files show as merge changes.
- **Git → Stashes…** opens a box of every stash, with its branch and age, to search by message or branch: ⏎ applies the
  one chosen, ⌘⏎ pops it and ⌘⌫ drops it. The stash commands are in the **Git** menu and the command palette too.
- A thin bar under the title moves while a Git command runs. A toast says when a commit is made, with an **Undo**
  button, and when a push or a pull is done.
- A clean work tree says so, and offers to push or pull when the branch is ahead of its upstream or behind it.
- A folder outside a repository gets an **Initialize Repository** button.

### Submodules and repository selection

Initialized submodules, including nested submodules, appear as separate,
collapsible sections in one continuously scrolling Source Control list. The
current repository's header stays at the top while its changes scroll beneath
it. The current change group (such as Staged Changes or Changes) stays directly
beneath that header, with its counts and actions, until the next group or
repository pushes it away. Headers combine the repository name, branch or
detached HEAD, and repository actions; a narrow sidebar keeps remote actions in
each repository's **⋯** menu. Each header's folder icon opens **Switch Worktree…** for its repository.
Repositories with changes start expanded; clean repositories start collapsed and
can be expanded to show their retained commit draft and a disabled commit button.
The arrow expands or collapses a section; clicking its name selects its history.
Each repository keeps its own commit draft, selection and Git operations.
Staging, unstaging, discarding and committing apply to that section's repository.
Parent entries distinguish a changed submodule commit reference from uncommitted
content inside it. Commit the submodule's files first, then stage and commit the
updated reference in the parent separately. Uninitialized submodules remain
entries in the parent until initialized externally.

Changes and history scroll independently. Until the divider is dragged, changes
fit their content up to half the view, leaving more room for history when most
repositories are clean. Dragging the divider saves its position with the
workspace's layout.

With multiple repositories, the lower part of Source Control shows the selected
repository's commit graph. The repository button in its header and in the status
bar opens a searchable picker with repository names and paths. **Auto** follows
the active file or diff; choosing a repository keeps its history selected as you
open other files. Each repository keeps its history selection, scrolling and
loaded page count. Diff tabs stay bound to the repository they were opened for.
The status bar picker replaces its folder item, so the root is not listed twice.
Destructive confirmation dialogs name their target repository, including in Auto
mode. Saved diff repository paths are relative to the workspace.
Discovery skips repositories without `.gitmodules` and refreshes on Git metadata
or `.gitmodules` changes, rather than enumerating indexes on a timer.
Opening a worktree containing submodules uses the same behavior; other worktrees
are opened separately as before.

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
**Changes** tab shows them. The button in the diff's toolbar or **Git → Show Files of Commits** shows a panel at the
tab's right, hidden at first, that lists those files in a tree of their folders or by path as **View as Tree** has the
changes: a click, or the arrows, scrolls the diff to a file, and the file at the top of the diff is chosen as it
scrolls. Drag its edge to widen it; the same button or menu item hides it again, in every commit's tab. The arrow at the start of a commit's row lists the files it changed under it,
→ and ← doing the same; a click on one shows the commit's changes at that file in the preview tab, and Enter keeps it.
The commit's menu checks it out (switching to
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

**Git → Compare Two Files…** compares two local files directly, including files outside the current repository. The
tab names the left and right paths, and each changed block has **Accept Left** and **Accept Right** arrows. Accepting
a side opens or reuses that file's document and records one undoable edit; save it from the file tab when ready. This
comparison reads and writes local documents only and does not stage Git changes.

Removed and added lines show in one column, or side by side with the lines of a change across from each other. Side by
side, a gutter between the sides joins each change's lines on the left to its lines on the right with a band, which
curves where the sides stand apart, and holds the change's arrows; pointing at an arrow lights the lines it takes. The
layout button in the breadcrumbs chooses side by side, one column, or side by side when each side has room for 80
columns, which is the default. Words that changed within a line are tinted,
and a bar along the left is brightest for the change you are on. Long lines wrap as the editor's do: **Word Wrap**
sets them all, and ⌥Z or the wrap button just the tab.

Double-click a line to open the file there. The arrows go from change to change, and the status bar says which change
of how many you are on, with the lines added and removed. At the last, or first, change of a file the navigation says
so, and one more press that way goes to the next file with changes, or the last of the previous; **diffCrossFiles** in
the settings has it cross at once, or never leave the file. At the first or last file it says so, and stops. With the
diff focused, keys work as in a pager:

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
| Shift+`j` `k`, Shift+↓ ↑ | Choose more rows, as do Shift-click and a drag |
| `c` | Comment on the rows chosen, or edit the comment chosen |
| `n` `N` | Go to the next or previous comment |
| Delete | Delete the comment chosen |
| ⌘C | Copy the lines chosen with their path and line numbers |
| Escape | Choose one row again |

### Review comments

To collect code for an agent from ordinary files, set `"editorAgentContext": true` in **Settings**
(off by default). Hover a line and click **+**, or select several lines first, to open the same comment
card used in reviews. While writing the comment, the source is shown with room for the card under those
lines; saving or cancelling returns to editing the source. Use ⌘Enter to save or Escape to cancel.
Leave the comment empty and choose **Add to Agent** to collect just the code. Collect snippets across
files, then use **Send N Items** above the editor to paste them, together with review comments, into the
last-used terminal without pressing Enter. The **…** button lists the collected items for navigation,
editing and removal. With no terminal open, sending copies the batch to the clipboard and keeps it.
Pending items survive restarts and turning the setting off.

Review an agent's changes, then hand it the review in one paste. Choose lines in a diff and press `c`, or click the
**+** by the line under the pointer, or select lines in a file and press ⇧⌘M (**Add Review Comment**), and type a
comment: ⌘Enter keeps it, Escape drops it. In a diff the comment shows under its lines, with buttons to edit or delete
it, and a bar by the line numbers marks its lines. In a file the bar marks them too, and the comment shows after the
last of them: click it, or run the command on its lines, to edit it. The scroll bar marks where each comment is.
Side by side, a comment shows under the side of its lines. While a comment is edited, **Delete** deletes it, and
the toast that says a comment was deleted, or every comment cleared, has **Undo**. Comments work in the diff of a
commit too. The status bar counts them, with a menu of them under their files to go to each. In a diff, `n` goes to
the next comment and `N` to the one before; **Go to Next Review Comment** and **Go to Previous Review Comment** do so
from anywhere, in the order of their files and lines. Going to a comment on lines removed, staged or of a commit shows
it in its diff.

**Send Review Items to Terminal**, from the button in the diff's toolbar, that menu, the **Git** menu or the
command palette, pastes every comment into the terminal used last, without pressing Enter, so you can add to it before
it goes to the agent, and clears them. **Copy Review Items** copies them instead. Each says where its lines are, relative to the folder, then shows
them, as a diff when it was made in one, then the comment, numbered when there are more than one:

````
cmd/mayoi/diffview.go:1228-1229
```diff
-	if ev.Kind != ui.InputKeyDown || ev.Mods&^ui.Shift != 0 {
+	if ev.Kind != ui.InputKeyDown {
```
Not here, the check belongs in the section before.
````

A comment follows its lines as the file changes. When they are no longer in the file, it says it is outdated, and
quotes the lines as they were. Comments not sent are kept with the folder's session, so they are there again at the
next start.

Commit menus also offer **Create Patch…** and **Copy Changes (Patch)**, including binary changes and root commits.
**Compare HEAD to Here** and **Compare Here to HEAD** open the changes between the two in a diff tab, inline or side by
side as the changes show, in the chosen direction; **Select for Compare** then **Compare with Selected** compares two
commits from the same repository. A comparison's card names both commits, each a chip that copies its hash, and its tab
comes back with the session. **Open Commit on Remote** supports
GitHub, GitLab and Bitbucket, preferring a unique remote decoration, then the current upstream, origin or the first remote.

### Branches and remotes

Source Control's **REPOSITORY** panel sits below **COMMITS**, starts collapsed and remembers whether it is open and its selected tab.
**Branches** lists the current branch first, then the most recently committed branches, with their upstream and ahead/behind
counts. Expand a branch for its history and any incoming or outgoing commits; **Load more** reads another page.
**Remotes** lists the configured remotes, each with where it points. Expand a remote to read its fetch and push URLs and
remote branches.
Panel data loads when it is opened, and branch history loads when that branch is expanded.

Branch rows offer checkout and pull; a remote branch's **Fetch Remote** fetches its whole remote. Context menus offer
branch creation, rename, deletion, merge and rebase, and remote creation, URL editing, removal, fetch and prune.
Deletion refuses unmerged branches. **Refresh** rereads local refs; **Fetch** contacts the remote.

When the branch has a pull request on GitHub, its number, how its checks went and its review show beside the branch in
source control and the status bar; click it to open its changes in a tab, or right-click it for **Open on GitHub**. It
comes from the `gh` CLI, run where the repository is, a host's for Remote SSH, and shows nothing without `gh` or its
sign-in. It is asked for again every minute while the window shows, every 30 seconds while checks run.

A commit of the history that is the head of a pull request open shows `#123` first among its branches and tags, muted
for a draft; click it to open the pull request's changes in a tab, or right-click it for **Open on GitHub**. The pull
requests come from `gh` the same way, read in the background after the history, again as the refs change and every two
minutes, so the history never waits for them; without `gh` or its sign-in, no commit shows one.

**Git → Open Pull Request…**, also in source control's **⋯** menu, lists the pull requests open, the branch's first;
type to find one by its number, title, author or branch, or type any number to open that one, whatever its state. Its
tab shows what GitHub shows, the changes from where its head left its base to its head, inline or side by side like
any diff, under a card of its state, its branches and **Open on GitHub**. Commits Mayoi does not have yet are fetched
from the remote of the pull request's repository to `refs/mayoi/pull/<number>/head` and `…/base`, which keeps them for
the tab when Mayoi opens again; a pull request from a fork needs no remote of its own. Each file's header has a
**Viewed** box, which marks it viewed on GitHub, as the box on GitHub does, and folds it; the files viewed show
folded as the tab opens, and the card counts them. When the pull request has commits newer than the tab's, the card
offers **Reload**.

**Git → Show Pull Request Checks…**, also **Show Checks…** in the badge's context menu and source control's **⋯**
menu, or a click on the badge's checks icon, lists the checks of the branch's pull request, failed first, and reads
them again every 30 seconds while one runs; type to find one. A failed GitHub Actions job offers **Send to Terminal**
(⏎), which reads the last 3000 lines of its log, without colors or time stamps, and pastes them into the terminal used
last without pressing Enter, as **Send Review Items to Terminal** does, copying them with no terminal open; **Copy
Log**; **Re-run** (⌘⏎), which runs that job again; and **Open on GitHub**, the only one a check outside GitHub Actions
offers.

The sync button in the status bar shows how far the branch is ahead of its upstream (↑) and behind it (↓), and pulls
then pushes in one click; a branch without an upstream has a publish button instead. Switching waits until open
files are saved.

**Switch Branch…**, from the branch in the status bar, at the top of source control or in the **Git** menu,
opens a box of the branches: the one checked out first, then the local ones, then those of the remotes, each the one
committed to last first. Each shows the author of its last commit, how long ago it was made and its subject. Type to find one by
name, or type a new name to create a branch from the one checked out. **Create Branch…** at the top of the unfiltered
list opens a name prompt. With matching branches, Enter switches to the first match; with no matches, it creates the
typed name. Choosing a remote's branch checks out a local branch that tracks it. Fetch, pull and push are also
available in source control and the **Git** menu.

Pull only fast-forwards. Push sets the upstream the first time. Git never prompts for a password inside Mayoi: it uses
your SSH agent and credential helper, and if those fail, the error says to run the command once in the terminal.

## Image viewer

PNG, JPEG, GIF, WebP and BMP files open in a viewer, fitted to the view on a checkerboard.

- Scroll with `⌘` (macOS) or `Ctrl` (Windows/Linux) held to zoom the image, or use **Zoom In Image / Zoom Out Image** in the command palette.
- Click the status bar's zoom percentage or run **Fit Image to View** to fit the image again.
- `⌘=`, `⌘-` and `⌘0` (macOS), or `Ctrl+=`, `Ctrl+-` and `Ctrl+0` (Windows/Linux), zoom or reset the whole window, including the sidebar.
- Double-click toggles between fitted and actual size.
- Drag or scroll to pan an image larger than the view.
- The image reloads when the file changes.

An SVG opens as text. ⇧⌘V, or the button at the end of the breadcrumbs, shows it as an image that updates as you type.

Markdown (`.md` and `.markdown`) opens as text too. ⇧⌘V or the same button shows a read-only, scrollable preview with headings, formatted text, links, nested lists, task lists, quotes, highlighted code, tables and local images. Split with ⌘\ to keep source in one column and a live preview in the other. **Open Preview to the Side** does both at once, the text keeping the keyboard; it uses the column beside the file, right or else left, when there is one. The preview scrolls with the text beside it, and the text with the preview as you scroll it. Tables are as wide as their text, and the status bar counts the words and the minutes to read them. GitHub's alerts, `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]` and `[!CAUTION]`, show with their icons and colors; a block of code has its language in the corner and a Copy Code button under the pointer; a link to `#a-heading` scrolls to that heading. Preview state is restored with the session. Remote images show their alt text without network requests. A path starting with `/` is taken from the root of the file's Git repository, or else of the workspace, as GitHub takes it. HTML comments and front matter are hidden, `<br>` breaks the line, and other HTML is displayed as text.

## Columns

The editor splits into columns side by side, each with its own tabs, and you resize them by dragging the handle between
two columns. A file can show in several columns at once. Those columns share its text and undo history, and each keeps
its own caret and scroll position. Drag a tab along its strip, into another column, or to the edge of a column to give it
a new column.

Choose **Lock Column** in a tab's menu to keep a column for terminals, or **Move to Locked Right Column** in a
terminal tab's or panel terminal's menu to move the running terminal there in one step. The lock in the tab strip unlocks
the column. Files, images and diffs opened from the Explorer, Search, Quick Open or Source Control, and terminals from
**New Terminal in Editor Area**, go to the unlocked column that has the keyboard, or the one used last when a locked
column has it. Dragging a tab or file into a locked column still puts it there. Closing files keeps at least one unlocked column beside the locked ones, without widening
them, and ⌘W closes an extra empty column. A locked column stays when its last terminal exits and offers **New
Terminal**. Locks, widths and the last file column come back with each project's session. The command palette has
**Toggle Column Lock** and **Move Terminal to Locked Right Column**, which take keyboard shortcuts.

Files dragged from the Finder open where you drop them: in a column after the tab shown, on its tab strip where the
marker shows, or on the edge of a column in a new column there. Dropped elsewhere, as on the explorer, they open in the
file workspace, avoiding locked columns. A dropped folder opens as `mayoi <dir>` opens it.
On macOS, files and folders dropped on Mayoi's Dock icon open the same way as `mayoi <path>`.

Files dragged from the explorer, from source control's changes or from the search results open in the same places. A
search match opens with the match selected.

## Terminal

Ghostty's emulator (libghostty-vt), in the panel or in a column tab. The panel is below the editors; **View → Panel
Position**, the command palette's **Move Panel to Left**, **Right** and **Bottom**, and `keybindings.json`'s
`workbench.action.positionPanelLeft` and its neighbours, and **Panel Position** in the menu of the panel's header, put
it at a side of the window. There it sits past the sidebar,
so with the sidebar on the same side the panel is the outer one, and dragging its inner edge sizes it. The title bar's
terminal button is on the panel's side too: left of the sidebar's button when both are on the left, at the right of the
bar otherwise. You can run several terminals, move one between the panel and a column by dragging it or from its menu,
and maximize the panel. **View → New Terminals in Editor Area**, `terminalLocation` in `settings.json`, has New Terminal
and the Explorer's **Open in Terminal** open a tab of the editor area rather than the panel; Toggle Terminal and the
panel's **+** still use the panel. Terminal colors follow the theme. With `terminalBackend` set to `"hc"`, shells run in
herdr-core and keep running after Mayoi quits, and the next start attaches to them again. A dot on such a terminal's
tab and chip says so, and pulses while its shell starts or its machine is away. **View → Install hc…**
installs or updates `hc` in `~/.local/bin` with herdr's installer, on macOS and Linux.
Files dropped on a terminal type their paths, quoted for the shell. ⌘V with only an image on the clipboard sends an empty
bracketed paste, as VS Code's terminal does, so that Claude Code pastes the screenshot. Selecting text copies it, as
in Ghostty and iTerm2: a drag, a double click for a word, a triple click for a line, Shift with a drag in a program
that takes the mouse, or Select All. `terminalCopyOnSelect` turns it off.

Command-click a file path in a local terminal (Control-click on Linux and Windows) to open it in Mayoi, including
files Git ignores. Recognized paths use forward slashes: `/absolute/path`, `~/path`, `./path`, `../path`, or a
relative path containing a slash. A filename with a `:line` or `:line:column` suffix works too; the suffix places
the caret there. Relative paths use the directory the shell reports, or the terminal's starting directory when
it reports none. Windows drive-letter and backslash paths are not recognized yet. Holding Command (Control
elsewhere) over a recognized path underlines it and shows a hand cursor.

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
- **New Window** (⇧⌘N) opens a window showing nothing, which the welcome page fills.
- **Star on GitHub**, at the foot of the welcome page, opens Mayoi's repository in the browser.
- **Open Folder in New Window…** (⌥⌘O), or ⌘-clicking a recent folder (or pressing ⌘↵ on it), opens it in a new window.
- **File → Open Recent** lists the last ten folders, except the window's own and any no longer on disk, and has
  **Clear Recently Opened**. On macOS the Dock icon's menu lists them too, under **New Window**.
- ⌃R (or **Open Recent → More…**) finds a recent folder by name: ↵ opens it in the current window, ⌘↵ in a new window.
  When a terminal has the keyboard, ⌃R goes to the shell instead.
- Opening a folder that another window already has brings that window to the front instead.

### Project tabs

**View → Project Tabs** keeps every folder open in one window instead, so you don't switch windows to change projects.

- A rail on the far left has a square for each project, the one shown marked with a pill along the rail's edge, and a
  dot on any with unsaved changes. Its tip gives the project's name, its folder and the keys that show it. The rail
  shows while the window has one project too, with the button that opens another folder after the squares.
- Each square shows the project's favicon or logo, or its folder's initials when it has none, found in its
  well-known icon files, `favicon.ico` included, a mygo app's icon (`mygo.json`'s `icon`, or `resources/icon.png`),
  the icon its sources declare (`site/index.html` and `docs/index.html` among them), or a macOS app's `.icns` or Xcode
  AppIcon, also on a connected host over SSH.
- Initials are two letters, and projects that would share them are told apart by the first letter that differs:
  Downloads and Documents show as "Dw" and "Dc". A folder on another machine has a server badge in the square's
  lower corner, gray while its host is connected, amber while connecting and red while not.
- **Rename Tab…** in a square's context menu shows up to two characters of your own, an emoji too, in place of
  its initials or logo; empty brings them back. **Tab Color** tints the square in one of the terminal's colors.
- Drop a picture on a square, from the Finder or the Explorer, or pick one with **Choose Icon…** in its context
  menu, and the square shows it in place of the project's logo: a PNG, JPEG, WebP, SVG, ICO or ICNS, a remote
  project's too, even while its host is not connected. Mayoi keeps a copy in `project-icons` beside
  `settings.json`, so the picture may move or go. **Reset Icon** brings the logo back. An icon and a label of your
  own take each other's place.
- Drag a square along the rail to reorder the projects, and drop a folder from the Finder on the rail to add it.
- Clicking a square shows that project, with its tabs, columns, terminals, Explorer and source control as it left
  them, each editor with its caret and scroll position. Each project keeps its own layout: what its sidebar shows,
  whether it is open and how wide, and the panel's height. A new project starts with the layout of the one shown. ⇧⌘] and ⇧⌘[ (**Go → Next Project** and **Previous
  Project**) step along the rail, and ⌃⌘1…9 (Ctrl+Alt+1…9 on Windows and Linux) show one by its place.
- Opening a folder, with ⌘O, ⌥⌘O, Open Recent, the welcome page or `mayoi <dir>`, adds it to the rail, or shows
  it if it is there already. The `+` after the squares opens one too.
- A square's context menu reveals the folder in the Finder, copies its path, renames, colors or gives an icon to
  the square, and closes the project or the others.
- **File → Close Project**, the square's context menu, or ⌘W with no tab left closes a project, asking about its
  unsaved changes first. Closing the window closes them all, and they open again at the next start, in the rail's
  order.
- Leaving a project saves its files that save themselves. Its terminals keep running out of sight, and one that rings
  its bell or sends a notification (OSC 9 or 777) marks the project's square in the accent until it shows again.
- Turning Project Tabs on gathers the open windows into the one in front, and turning it off parts the projects into a
  window each again.

## Remote SSH

**File → Remote SSH → Connect to Host…**, the welcome page or the command palette opens a folder on another machine
over SSH, with the system's `ssh` and nothing to install on the other side. It works on macOS and Linux; Windows does
not have it yet.

- The box lists the hosts connected to before under **Recent**, each with the folder opened there last, then the hosts
  of `~/.ssh/config`, with those it includes. Each shows the user and the machine `ssh -G` resolves it to. Type to
  filter them, or type `user@host` or `user@host:port` to connect to one it does not list. While it connects, the box
  says as whom, and after a few seconds for how long; Escape goes back to the hosts.
- Once connected, the box lists the folders of the host, from the folder opened there last, or the home folder. Type a
  path (`~` for the home folder), ↑↓ choose a folder, ↵ or Tab goes into it, `..` goes up, and ↵ on the first row opens
  the folder typed. The folder opens as **Open Folder** opens one: in the window if it shows nothing, else in a window,
  or a project tab, of its own.
- Mayoi signs in with keys and the SSH agent, never a password, and honors `~/.ssh/config`: users, ports, keys,
  `ProxyJump` and the rest. A host whose key is not trusted yet needs one `ssh` from a terminal first. One connection
  goes to each host (an ssh ControlMaster), shared by every window and command, and ends with the last window of the
  host.
- The Explorer, quick open, search and replace in files, Source Control, blame, the gutter marks and diffs work as in a
  local folder, running `git`, `rg` (or `grep`) and `find` on the host. Terminals are shells of the host.
- The folders in a folder of the Explorer are read along with it, at one round trip, but those the index skips, as
  `node_modules`, so that they open at once, without waiting on the host; one read more than a few seconds before
  shows at once and is read again.
- Files open at once, read in the background, and save in the background: a save goes whole to a temporary file on
  the host and is copied into the file only once it all arrived, so the file keeps its owner, mode and links, and a
  connection lost on the way leaves it as it was. A save first checks, in the same round trip, that the file is as it
  was read: one changed on the host since asks whether to overwrite it or to revert to it, which Undo takes back.
- Changes on the host show as they would locally: `inotifywait` reports them where the host has it (the
  `inotify-tools` package), and otherwise the folders shown and the files open are looked at every 3 seconds, every 15
  while the window is in the background, and at once as it comes back. Without
  inotify, a change within the same second that leaves a file's size alone can go unseen on hosts whose `stat` has no
  sub-second times.
- The status bar shows `SSH: host`, red while disconnected, with a menu to reconnect, disconnect, open another folder
  of the host or close it, and the title bar names the host. **Disconnect** keeps the window and its tabs: the files
  are read-only, with a banner to reconnect, until it connects again, and their unsaved changes stay.
- A connection that drops, as the network goes or the Mac sleeps, does the same, and connects again by itself: after 2
  seconds, then twice as long each time, up to every 30 seconds, and at once as the window comes back to the front. The
  banner says when it tries next, with **Reconnect Now**. A host that refuses the keys, or whose key is not trusted,
  is not tried again until asked. Reconnecting reads the tree, the open files and the status again. The terminals of
  the host stay, with what they showed, and say the connection went; they start again as it comes back, in the
  folder their shell was last in when it says so, with what they showed above.
- Files copied in the Finder, or in the explorer of another window, paste into a folder of the host with ⌘V or
  **Paste**, and files dropped from the Finder on a folder of the explorer copy into it: they go up as one tar stream
  through the connection, land beside the folder, and take their names, as copies when a name is taken, once they all
  arrived. A remote file copied in the explorer puts its path on the host on the clipboard, and pastes into another
  folder of the host as a copy made there, into a folder of this machine as a download, and into a folder of another
  host by way of this machine. **Upload…** in the explorer's menu copies files chosen in a dialog into a folder of the
  host, and **Download…** a file or folder of the host into a folder of this machine, the Downloads folder first,
  with a button to reveal it once it is there. The status bar shows each transfer and how far it got, and a click on
  it cancels one: what arrived of it is taken away.
- A terminal of the host pastes files of the Finder, of the explorer of a local folder, or dropped on it, as copies
  uploaded to a temporary folder of the host, and types their paths there. An image alone on the clipboard, as a
  screenshot, goes up as a PNG file of its own, so that Claude Code running on the host takes it as it does a
  screenshot pasted on the Mac. A file of the host dragged to its terminal types its path on the host.
- Ports of the host forward to this machine, so that a server running there, as a web app in development, opens in
  a browser here. Clicking a link to `localhost`, `127.0.0.1` or `0.0.0.0` in a terminal of the host forwards its port
  and opens it; **Forward a Port…**, in **File → Remote SSH**, the command palette and the status bar's `SSH` menu,
  forwards one by its number. A port goes to the same port here when it is free, else to another, and the status bar
  shows the ports forwarded, with a menu to open each in the browser, copy its address or stop forwarding it. Each
  connection goes through the host's ssh connection, so a forward works again once a host lost connects again, and
  ends as the last window of the host closes.
- Reveal in Finder is left out of a remote folder, and deleting a file deletes it for good, after asking, as a remote
  folder has no trash.
- Recent folders, the windows and the tabs of a remote folder are kept as those of a local one, and a remote folder of
  the last session connects again as the app starts.

## The `mayoi` command

**File → Install 'mayoi' Command** installs the command into `~/.local/bin` when that directory is on your `PATH`, and
otherwise into `/usr/local/bin`, which asks for your password. On Linux, `install.sh` puts it in `~/.local/bin`, and the
Debian package in `/usr/bin`.

- `mayoi .` opens the current folder, in a new window or the one that already has it, or with project tabs in a new
  tab.
- `mayoi <file>` opens a file in the window whose folder contains it.
- `mayoi -g <file:line[:column]>` opens a file with the caret at a line and column.
- `mayoi -d <file> <file>` compares two files, side by side or inline as the diffs are set to show.
- `mayoi -n [folder or file]...` opens them in a new window, and with nothing, a new empty window.
- `mayoi -w <file>...` returns once the files are closed, so that `git config core.editor "mayoi -w"` edits commit
  messages in Mayoi.
- `mayoi --remote <host:/path>` opens a folder on an SSH host.
- `mayoi run <command>...` runs commands by the identifiers that `keybindings.json` names them by, in order, as
  `mayoi run view.terminal git.pull`. Some take an argument after `=` in place of what they would ask in the window:
  `file.openFolder=<folder>`, `file.openFolderWindow=<folder>`, `file.saveAs=<file>`, `go.line=<line[:column]>`,
  `go.quickOpen=<text>`, `view.selectTheme=<theme>` and `remote.forwardPort=<port>`. An unknown command, or an
  argument one doesn't take, runs none of them.
- `mayoi commands` lists the commands, a line for each, with its identifier, name, shortcut and argument, apart by
  tabs.
- `mayoi --help` lists the options, and `mayoi --version` prints the version.

A command acts on the window whose folder contains the directory it runs in, else the one in front; `--window
<folder>` picks the window that has the folder open. The command returns as soon as Mayoi has done what it asked, but
with `-w`, and starts Mayoi if it isn't running. When something goes wrong, it says so and exits with status 1, or 2
for options it doesn't know. A `mayoi` command an older Mayoi installed is brought up to date as Mayoi starts.

## Appearance and persistence

**View → Theme** picks a theme, Mayoi, The Matrix, Claude, Qoder, Grok Bot, Cursor, Purple Gold, or one of the Halloween
themes, Jack-o'-Lantern, Witch's Brew and Blood Moon, and apart from it whether the window follows the system's light or
dark mode or is always Light or Dark. Light and Dark are greyed out under The Matrix, Purple Gold and the Halloween
themes, which are always dark.
The Matrix is phosphor green on black, with the whole window in the code's font, file icons in greens, and red and amber
kept for what is removed, failed or changed. Digital rain falls behind the welcome page while the window has the
keyboard, and holds still without it or when the system asks for reduced motion. Claude is Anthropic's ivory and warm
near-black with Claude's orange, in a light and a dark kind. Clawd, Claude Code's character, takes the logo's place on
the welcome page and sits at the start of the status bar: it blinks now and then, and hops while a Git command or a search of the files
runs, and holds still as the rain does. Qoder is Qoder's Forest theme: white or green-tinged greys with forest green,
in a light and a dark kind, and its code in Forest's greens, blues, teals and ambers. Qoduck, Qoder's duck, takes the
logo's place and the status bar's start as Clawd does, and blinks, bobs and hops as it does. Grok Bot is the Grok Bot
app's Sand theme: white or near-black greys, near-black or near-white buttons and its blue for the accent, in a light
and a dark kind, and its code in the warm colors of x.ai's terminal. Grok Bot's face, a round head with two slanted
eyes, takes the logo's place and the status bar's start as Clawd does, looks up as it hops, and now and then glances
to one side and the other. Cursor is Cursor's own Cursor Light and Cursor Dark: near-white or near-black greys with
Cursor's blue for the accent and the buttons, in a light and a dark kind, and its code in Cursor's own colors. It has
no character, so the welcome page keeps Mayoi's logo. Purple Gold is a near-black sidebar, tab strip and panel around
a charcoal editor, with a royal purple status bar, classic gold for the accent, the buttons and the chosen rows, and its
code in golds, lavenders, roses and teals. It has no character either. Jack-o'-Lantern is soot-black chrome around a
warm editor, pumpkin orange for the accent and the buttons, and a status bar that glows like the lantern's face, with its
code in candle yellow, ember and vine green and witch-purple keywords. Witch's Brew is plum chrome with a witch-purple
status bar and slime green for the accent and the buttons, with its code in violets, slime greens, teals and lilacs.
Blood Moon is near-black with a red undertone, crimson for the accent and the buttons, a dark-blood status bar, and a
scarlet apart from the crimson for what loses something, with its code in reds, old gold and moonlight blue. Each has a
character drawn from cells in its accent, which takes the logo's place and the status bar's start as Clawd does: a
carved pumpkin whose face glows, a cauldron whose brew bubbles, and a bat that flaps its wings as it hops.
**View → Theme → Color Theme…** (`workbench.action.selectTheme` in `keybindings.json`, as `cmd+k cmd+t`), or **Color
Theme…** on the welcome page, opens a box of the themes, each with a little window in its colors and its character: the
window takes each theme as ↑ and ↓ move to it, and ← and → switch the appearance; Return keeps them, and Escape goes
back to those the box opened on. From one theme or appearance to another, every color of the window fades to the new
one's, at once when the system asks for reduced motion. **View → Sidebar on Right**
moves the sidebar to the right of the editors. It remembers the sidebar's and the panel's sides, the sidebar and panel
sizes, and every window with its folder, frame, columns and tabs, each tab with its caret, selection, scroll position
and own word wrap, and the folders open in the Explorer. Started without a folder, Mayoi reopens the windows that were
open when it quit.
A recent folder closed, by closing its window, **File → Close Folder** or opening another folder in its window, opens
again with its tabs, columns and Explorer as they were. Closing the last window quits the app.

## Settings

**Mayoi → Settings…** (⌘,), or **File → Settings…** on Windows and Linux, opens the settings window: every preference
by category, with search, a mark on those changed from their defaults and a button to reset each. A change applies in
every window at once: the code's and the terminal's fonts, weight, ligatures and line height, the theme and appearance,
auto save, word wrap, the diff layout, vim and more.

**File → Open Settings (JSON)** opens `settings.json` in a tab instead. Saving it applies it the same way. Mayoi keeps
the windows, tabs, recent folders and layout itself, in `state.json` beside it. See [SETTINGS.md](SETTINGS.md) for
every key and where the files are.

A folder can set some preferences for itself, such as `vim` or `autoSave`, in `.mayoi/settings.json`. Choose the folder
at the top right of the settings window, or use **File → Open Folder Settings (JSON)**. See
[Folder settings](SETTINGS.md#folder-settings).

## Keyboard shortcuts

**File → Keyboard Shortcuts…** lists every command with its keys in the settings window. Search by name or by pressing
keys, click a command and press its new keys, and see which other commands already use them. Each change goes to
`keybindings.json` at once, keeping its comments.

**File → Open Keyboard Shortcuts (JSON)** opens `keybindings.json`, in VS Code's format, so a VS Code or Cursor file can
be pasted in. Saving it applies it in every window at once. It binds keys and chords to Mayoi's commands or VS Code's
names for them, with `when` conditions. See [SETTINGS.md](SETTINGS.md#keybindingsjson) for the format, and for the
shortcuts that differ on Windows and Linux.

Some commands are only in the command palette: **Copy Path of Active File**, **Copy Relative Path of Active File**,
**Reveal Active File in Finder** (File Explorer on Windows), **Indent Using Tabs** and **Indent Using 2, 4 or 8 Spaces**. **Selection → Sort Lines
Ascending**, **Sort Lines Descending** and **Transform to Camel Case** work on the lines selected, or every line, and on
the words selected, or the caret's.

## Settings sync

**Mayoi → Sign In…** (**Help** on Linux and Windows, and the command palette's **Account** commands)
signs in to your [Mon](https://mon.sorrycc.dev) account, with GitHub or Google. Mayoi shows a short code and opens the
browser to approve it. Your preferences and `keybindings.json` then sync between your machines: a change saves to the
account a couple of seconds after you make it, and Mayoi gets the account's changes as it starts and as it comes to the
front. Changes made on two machines merge setting by setting.

A cloud at the right of the status bar shows whether the last sync went through, and opens the account's panel: when
the machine last synced, **Sync Now**, switches to keep the settings or the keyboard shortcuts on this machine, and
**Sign Out**. A sync that couldn't reach the account tries again by itself, and `keybindings.json` changed on two
machines opens side by side to merge. See [SETTINGS.md](SETTINGS.md#settings-sync).

## Updates

Release builds check GitHub Releases for a new version every hour, the first time 10 seconds after launch and again
when the computer wakes. A new version shows in the status bar, on the welcome page, and in the menu as **Mayoi → Update
to 0.12.0…** (**Help → Update to 0.12.0…** on Linux and Windows); without one the item reads **Check for Updates…** and
checks right away. Clicking any of them opens a window with the release notes that offers to install the update, skip
that version, which hides it, or remind you later. Updates download only what changed when they can, are checked
against Mayoi's signing key, and apply when Mayoi relaunches. Development builds, and copies Mayoi can't write to, never
check.
