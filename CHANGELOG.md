# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## 0.11.11

- The welcome page has a **Star on GitHub** link at its foot, which opens
  Mayoi's repository in the browser.
- A click on a tab of another column while the find box is open no longer
  crashes Mayoi: the box moves to that column, rather than show in both.
- A tab or a terminal dragged from one window and dropped in another stays
  in its window, rather than show in both, which crashed Mayoi as the
  terminal took the keyboard.
- A crash log of a version from before 0.11.6, which doesn't say its
  version, is reported as of an unknown version, rather than of the one
  sending it.

## 0.11.10

- The diff's toolbar tells its buttons apart: the Whitespace menu shows ¶,
  the Context menu a folded list, the Highlight menu two letters, and the
  button for a commit's files a tree. The split toggle is gone, as the
  Layout menu chooses side by side, one column or auto.
- A commit's tab no longer shows the panel of its files at first: the
  button in the diff's toolbar, or **Git → Show Files of Commits**, shows
  it, and it stays shown in every commit's tab until it is hidden.
- **⌘F** and **⌥⌘F** select the query the find box has, as VS Code does,
  so typing replaces it: when the box opens again with the last query, and
  when it has the focus already.

## 0.11.9

- A sideways swipe scrolls the result pane of a merge, as the side panes'
  does; the swipe used to be lost on the result editor's own sideways
  scroll, which no scroller of the merge tracks.
- The panes of a merge wrap their lines as the Word Wrap setting says, and
  ⌥Z wraps or unwraps the merge of the tab alone. The marks of the
  conflicts, the click that chooses one, the jump that reveals one, the
  gutter's bands and buttons and the panes' scrolling together follow the
  rows a line wraps to, and every line of a wrapping pane is laid out for
  its height, so that revealing a far conflict does not land on an
  estimated height and show a blank. Each pane wraps at its own width,
  as the split diff's sides do not: a conflict's marks can start at a
  different height in every pane, and the gutter's bands slant to join
  them.
- The Grok Bot theme: **View → Theme → Grok Bot** takes the Grok Bot app's
  Sand Light and Sand Dark colors, light or dark as the Appearance is:
  white or near-black greys, near-black or near-white buttons, and its blue
  for the accent, with the code in the warm colors of x.ai's terminal.
  Grok Bot's face, a round head with two slanted eyes, takes the logo's
  place on the welcome page and sits at the start of the status bar,
  blinking now and then and hopping while Git or a search works, as Clawd
  and Qoduck do, and glancing to one side and the other now and then.
- **View → Theme → Color Theme…**, or **Color Theme…** on the welcome page,
  opens a box of the themes, each with a little window in its colors and
  its character. The window takes each theme as ↑ and ↓ move to it, and ←
  and → switch between the system's appearance, light and dark: Return
  keeps them, and Escape goes back. `workbench.action.selectTheme` binds it
  in `keybindings.json`, as `cmd+k cmd+t` does in VS Code.
- Changing the theme or the appearance fades every color of the window to
  the new one's, rather than switching at once, but with reduced motion.
- A commit's tab lists the files it changed in a panel at its right, in a
  tree of their folders or by path as **View as Tree** has the changes:
  click one, or choose it with the arrows, to scroll the diff to it, and
  the file at the top of the diff is chosen as it scrolls. Drag its edge to
  widen it; the button in the diff's toolbar, or **Git → Show Files of
  Commits**, hides it. Comparisons of two commits have it too.
- The arrow at the start of a commit in **COMMITS** lists the files it
  changed under it, the graph running past them, and → and ← do it from
  the keyboard. Click a file to see the commit's changes at it in the
  preview tab; Enter or **Open Changes** keeps it in a tab of its own.

## 0.11.8

- **View → New Terminals in Editor Area**, or `"terminalLocation": "editor"`
  in `settings.json`, has New Terminal and **Open in Terminal** open a tab
  of the editor area rather than the panel.
- The Markdown preview shows the HTML in Markdown as GitHub does, rather
  than its tags as code: pictures as wide as `<img width>` says, from a
  `<picture>` too, links, bold, italic and code, headings and paragraphs,
  and centers what `align="center"` centers, Markdown between the tags of
  a `<div>` included.
- The Markdown preview shows pictures of the web, as badges, fetching them
  in the background, and the pictures of a paragraph next to each other on
  one row; an SVG with text, which it would draw without its text, shows
  its alt text.
- The mode badge of vim mode in the status bar switches to the new mode's
  name at once, rather than fading the old name out over it, which broke
  up the letters of both for a moment.

## 0.11.7

- Untitled documents keep their text as Mayoi quits, and open again,
  changed, at the next launch, rather than asking where to save them, so
  restarting or shutting down the Mac no longer stops on them.
>>>>>>> origin/main
- Switching, creating and tracking branches, checking out a commit, and
  unstaging or discarding changes also work on hosts with Git older than
  2.23, which has no `git switch` or `git restore`.
- Long file paths in diff headers shorten to fit narrow columns, keeping
  filenames clear of change counts, status badges and file actions.
- Files copied in the Finder paste into a terminal as their full paths,
  quoted for the shell, rather than only their names.
- On macOS, holding a key down repeats it, in the terminal and the editor,
  rather than opening the accents of a letter.
- Linux: tabs indent code to their tab stops again, and text is dark and
  crisp again on Ubuntu 22.04, where it was faint and tinted; MyGo 0.6.0
  had undone both.

## 0.11.6

- After a crash, Mayoi asks at the next launch whether to send its crash
  report, so that it can be fixed: **Send**, **Don't Send** or **Show
  Report**, with **Don't ask again** kept as `crashReports` in the
  settings. A report is anonymous and has your home folder taken out. A
  crash log now starts with Mayoi's version and system, an error in the
  background, as reading git or searching, no longer quits Mayoi but is
  kept to report, and on macOS the system's own crash reports of Mayoi are
  kept too.
- Terminal tabs remain clickable across their full height when the tab
  strip overflows; the horizontal scroll bar has its own space below them
  only when needed, keeping the panel compact when the tabs fit.
- As the window narrows, the merge view's bars move their actions into a
  **⋯** menu one by one, the least needed first, instead of cutting them
  off.
- Clicking the branch in the status bar opens the same searchable branch
  picker as Source Control, with **Create Branch…** at the top; fetch, pull
  and push stay in Source Control and the Git menu. Invalid branch names are
  caught before Git sees them.
- A merge's three panes scroll together: the result pane no longer snaps
  back to its caret, pulling the others back to the top with it.
- The bands joining a merge's sides to its result stay inside the gutter
  instead of running up over the merge's header.
- Scrolling with Command held over a terminal no longer shrinks or grows
  its font, as it did when switching back to Mayoi with Command-Tab;
  `Cmd+=` and `Cmd+-` still size all the text.
- On Linux and Windows, a menu button in the title bar replaces the row the
  menu bar took. On Linux the window buttons take the theme's colors, and
  the terminal button sits beside them.
- Choosing Light or Dark appearance now holds when the desktop's color
  scheme is the other one.
- Maximizing the terminal panel at the bottom of the window fills the
  window again, instead of squeezing the terminal into a column one
  character wide.
- MyGo 0.6.0: the window redraws and lays out only what changed, the mouse
  wheel scrolls with a glide, the terminal shows images programs send
  (Kitty graphics) and takes the paths of files dropped on it, and menus
  take Windows 11's style there. Mayoi no longer builds on a fork of it.

## 0.11.5

- Discarding changes block by block in a diff now always goes through the
  file's document, also when the file was not open: the discard is one
  step of undo, the tab shows the file as changed until it is saved, and
  a diff that is behind the file on disk is read again rather than
  writing over what changed.
- Every block of a diff of the changes not staged has a round arrow, by its
  lines in either layout, that puts the lines of the version it is compared
  against in place of the block's own: the lines of the Index going into the
  work tree revert the block, in the file's document, as one step of undo,
  and the arrow says which versions it reads and writes. Blocks beside it
  keep their changes, the view stays near where it was as the diff is read
  again, and ⌘Z and ⇧⌘Z undo and redo from the diff's keyboard. The changes
  staged and a commit's take no arrows: the index goes on by staging, and
  the versions Git keeps are read only — a file that a diff adds or deletes
  whole says so under its lines, with what the buttons of its header do.
- Control-clicking the round arrow of a change of a diff of the changes
  not staged appends instead: the lines of the version it is compared
  against go after the change, as one step of undo, and the arrow says so
  while Control is held. **Append the Lines of the Change** in the Git menu
  and the command palette does the same to the change the diff has chosen.
- The working tree side of a diff can be edited right in the diff: press
  **I** on a change, or the **Edit** button, and the side takes typing,
  selection, copy, cut, paste and the usual editing keys, with the caret
  painted where it is. **Esc** goes back to reading. The edits go through
  the file's document, so the editor tab shows the same text, the save
  state is the document's (`Cmd+S` saves from the diff too, autosave
  included), and undo and redo are the document's one history. The diff
  reads the sides again as you type, keeping the scroll and the caret's
  line; its block actions refuse to run while it is catching up. The
  staged, HEAD and commit sides stay read-only, and a line of any side
  copies with `Cmd+C` while the diff reads as a pager.
- Diff tabs now expose **Previous File** and **Next File** controls alongside
  the existing change navigation, with matching commands in the Git menu, and
  a direct **Side by Side** toggle for the split viewer.
- Change navigation in diffs no longer wraps round the ends without a word:
  at the last, or first, change of a file it says so in the status bar, and one
  more press goes to the next file with changes, or the last of the previous.
  At the ends of the diff it says so, and stays. **diffCrossFiles** in
  settings.json has it cross at once, or never leave the file. The status bar
  says which change of how many the diff is on, and file navigation says where
  it is at the first and last file instead of jumping nowhere.
- Opening a file from a diff, with `e` or double-click, lands on the line of
  the change you are on, also from the change's `@@` header, after the layout
  changed, after its file folded and opened again, and after the sides were
  read again.
- Whole-file changes in a diff have explicit actions: a deleted tracked file
  offers **Restore File** from the index, while an untracked file offers
  **Remove File…** and follows the existing trash or remote-delete policy.
  These actions keep file existence distinct from an empty text buffer and
  refresh any open document after the operation.
- Diffs can filter comparison whitespace from the **Whitespace** menu:
  exact, line-end, all whitespace, or whitespace plus blank lines. The
  filter changes only what the diff shows and counts; source text and block
  actions continue to use the original lines.
- Diffs offer a **Highlight** menu that shows what changed within a line by
  word (as before), by character, or no more than the line, and a **Context**
  menu that keeps none to ten unchanged lines around each change and folds
  the rest away. Both are kept in the settings for the next diffs. What a
  diff highlights is only so: the changes its block actions take are still
  its hunks of lines, as the header of each run says.
- A side-by-side diff can let its sides scroll together, or each on its own:
  **Synchronized Scrolling** in the Layout menu. A wheel over a side scrolls
  that side alone; together again, the sides line up from the line the right
  side was at.
- The rows of a side-by-side diff's sides align by default, a line the one
  side lacks holding a space on the other; **Line Alignment** in the Layout
  menu, and `diffAlignedRows` in settings.json, turn that off: each side's
  lines close up where the other has lines it lacks, and the sides read in
  streams of their own, scrolling on their own.
- A diff keeps its place: folding or unfolding a gap or a file, wrapping its
  lines and a wider or narrower window keep the line at the top of the view
  the same line of the file, and a layout chosen by hand is no longer the
  width's to change. A file that folded under the view unfolds where the
  view was.
- **Compare Two Files…** in the **Git** menu opens a diff of any two local
  files, whether or not they are in the current repository. The tab names
  both paths and each changed block offers **Accept Left** or **Accept Right**;
  the chosen file is edited through its document history for undo, redo and
  save, without staging either file.
- Conflicted files open in a **three-way merge view**: the two sides read
  only beside an editable result that starts as the base, with the conflicts
  marked in all three panes. Each conflict takes **Accept Left/Right** or
  **Ignore Left/Right**, and **Resolve using Left/Right** decides every
  conflict of the file at once. The result edits, undoes and redoes like any
  document — undo brings the sides a conflict took back with it — and its
  unresolved count, Save and **Mark Resolved** (which stages the file) close
  the loop with the merge, rebase, cherry-pick or revert it belongs to. A
  resolved file goes back in conflict with **Restore Conflict…**, and a
  merge left and reopened goes on where its saved result stood.
- A merge that stopped for conflicts now works file by file: the merge
  view lists **the files in conflict** — resolved ones among them, with
  their resolution to take back — and says how many of them stand. The
  changes of a side that no conflict of the file holds are applied into
  the result as it opens (**Apply Automatically** in the Apply menu turns
  that off), or from the menu for both sides or a side's own, as one step
  of undo; the conflicts themselves stand for the hand to decide.
  **Apply Changes** saves the result, stages the file and opens the next
  file in conflict; **Save and Close** saves it and goes back to the
  list. The list shows of itself when a command stops for conflicts, and
  the merge goes on once every file of it stands resolved. The arrow keys
  and Enter choose and open its files, and a file whose conflicts still
  have the base's lines asks before it is marked resolved.
- A side-by-side diff has a gutter between its sides, as JetBrains' has:
  each change is a band from its lines on the left to its lines on the
  right, curving where the sides stand apart, and its arrows stand in the
  band by the first line of the side they read, larger and easier to hit;
  in the one column, an arrow stands by its change's first line too.
  Pointing at an arrow lights the lines it takes, and the lines an arrow
  or an append writes glow a moment as the diff shows them again.
- The words tinted as changed within a line are those that changed against
  the line it is paired with: lines that moved, as after an added import,
  were compared with the line at their own number on the other side.
- With **Line Alignment** off, a split diff scrolled down shows both sides
  again — the left one went out of view — and the right side follows the
  left as it scrolls, its changes going by at the pace of the left's lines.
- Going to the next or previous change, or file, glides there when it is
  near, and the buttons of a run of changes fade in and out with the
  pointer.
- The three-way merge view shows the whole text of each side — its side
  panes cut every line after a few characters — and its panes scroll
  together, a side's changes going by at the pace of the result's lines.
  Gutters between the panes join each change of a side to the result's
  lines, with **»** and **×** by each conflict to accept or ignore that
  side's lines, and **Apply This Change** by a change of a side's that no
  conflict holds. Its bars have outlined buttons with icons, a line under
  them fills as the conflicts are resolved, and going to a conflict glides
  there.
- The status bar has a Git sync button beside the branch, with incoming
  and outgoing commit counts. It pulls with fast-forward only before
  pushing that branch to its upstream, or publishes a branch that has
  no upstream yet.
- On Linux, Mayoi no longer crashes when opening a folder, Settings or
  anything else that changes the menus: the menu bar is replaced after
  the window has drawn, not while GTK is still drawing it (#51).

## 0.11.4

- Mayoi checks for a new version every hour, not once a day, and shows it
  in the status bar, on the welcome page and as **Update to …** in the
  menu, rather than opening the update window by itself: a click opens it.
- Mayoi signs in to Mon as the Mac's name, as in Sharing settings, where
  it gave its host name, `U-…-2015.local`, and a development build adds
  ` (dev)`, so Signed-in apps tells the two apart. Signing in again signs
  out a token Mayoi still keeps first, and Mon replaces Mayoi's earlier
  sign-in on the same computer, so it no longer piles up sign-ins there.

## 0.11.3

- The Markdown preview's text can be selected and copied, across
  paragraphs, lists, code and tables; a list's numbers stay out of it.
- **A redesigned Markdown preview:** headings scaled as GitHub scales
  them, code within text on a rounded chip, blocks of code on cards with
  their language in the corner and a Copy Code button under the pointer,
  tables whose columns are as wide as their text asks, with striped rows,
  and a picture not shown as its alt text on a chip.
- The Markdown preview shows GitHub's alerts, a quote that opens with
  `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]` or `[!CAUTION]`, with
  its icon, its title and its color.
- A link to a heading of the document, as `[Usage](#usage)`, scrolls the
  Markdown preview to it.
- **Open Preview to the Side**, in the View menu, the command palette and
  a tab's menu, shows the preview of a Markdown or SVG file in a column to
  the right, the text keeping the keyboard. VS Code's
  `markdown.showPreviewToSide` binds to it.
- An empty Markdown document's preview says there is nothing to preview,
  rather than showing a blank pane.
- A diff highlights the words that changed in a line modified below lines
  added or removed, rather than runs of another line's changes.
- The Markdown preview parses and draws faster, its text in fewer runs.
- A picture that fits the image viewer or an SVG preview casts a soft
  shadow.
- **The Markdown preview scrolls with its text**, shown in another
  column: scrolling the text, by the wheel, the keys, Find or Go to Line,
  brings the preview to the same place, block by block and within a
  block, and scrolling the preview brings the text along.
- Switching between a Markdown file's text and its preview keeps the
  place: the preview opens where the text was, and the text where the
  preview was.
- A Markdown table is as wide as its text, measured in the preview's
  font, with round corners, its cells in a row as tall as the tallest.
- The status bar of a Markdown preview counts the words of the text, a
  Chinese or Japanese character a word, and the minutes it takes to read.
- A picture alone in its paragraph that the preview does not show, as a
  remote one, is a card with its alt text and its file's name, which
  opens its link.
- The room above a heading of the Markdown preview takes in the room
  below the text before it, as on GitHub, rather than adding to it.
- An image zoomed in past its size shows its pixels as crisp squares.

## 0.11.2

- New Terminal in Editor Area opens in an unlocked column when a locked
  one has the keyboard, as files do.

## 0.11.1

- **Folders of projects in Source Control:** a folder that is not a Git
  repository but holds some, as `workspace/project-a`, or two folders deep,
  lists each one's changes to stage and commit, as submodules are, instead
  of offering to make the folder a repository. A project cloned or removed
  later is added or dropped (#17).
- On Windows, the Undo, Discard and Reset dialogs and the repository
  headers name a submodule with `/`, as `packages/module`, like the other
  systems.
- A project's square dragged along the rail goes where it is dropped
  again, before the square under the pointer, not always at the end.

## 0.11.0

- Settings sync no longer undoes a change to `settings.json` or
  `keybindings.json` made as it syncs, from a menu or by saving the file:
  it merges the change with the account's and syncs it. A save of
  `settings.json` no longer loses to the settings' own write of a moment
  before.
- Windows: a file opened from quick open, or by vim's `:e`, is the file
  the Explorer shows, with backslashes, rather than a second tab of it
  that the Explorer could not reveal.
- Windows: renaming refuses a name with `/` in it, as it does one with
  `\`, rather than moving the file into a folder of that name.
- Windows: the Markdown preview finds an image or link that starts with
  `/` in the repository's root, as on macOS, and shows one given as a
  whole path, as `C:\pictures\art.png`.
- Windows: Source Control's tree of changes keeps its folders for a work
  tree at the top of a disk, as `D:\`, rather than listing every file
  at its top.
- Windows: a file given to `mayoi` in other letters than its folder's,
  as `c:\proj\a.go` for `C:\Proj`, opens in that folder's window.
- Windows: a terminal is named after its shell, `pwsh`, `powershell` or
  `cmd`, rather than "shell".
- Windows: the repository picker finds a repository typed with `/`, as
  `packages/module`, as well as with `\`.
- The home folder shortens to `~` only for itself and the folders in it,
  not for a folder whose name begins the same, as `/Users/me2`; on
  Windows, whatever the case of its letters.
- The Windows fixes follow @wowyuarm's port of Mayoi to Windows (#40),
  and the tests now run on macOS and Windows too.
- The Markdown preview's column is half as wide again, 120 digits
  rather than 80, so tables and images have room in a wide pane while
  lines stay short enough to read; a narrower pane still fits it.
- Vim mode records macros: `qa` records the keys typed into register
  `a` until `q`, with the status bar saying so, and `@a` plays them,
  `3@a` three times, `@@` again and `@:` the last command. A macro stops
  where a motion fails, as in vim.
- Vim mode has registers: `"a` to `"z` yank, delete and paste on their
  own, `"A` appends, `"0` holds the last yank, `"1` to `"9` the last
  deletes of lines, `"_` keeps nothing, and `".`, `":` and `"/` what was
  typed, run and searched last. `:reg` shows them.
- Vim mode has marks, `ma` then `'a` or `` `a ``, which move with the
  text as it changes, and a jump list: `''` goes back, and ⌃O and ⌃I go
  back and forward through the places `G`, `%`, searches and marks
  jumped from. `:marks` lists them.
- Vim mode adds `:normal` to type keys over a range of lines, `:g` and
  `:v` to run a command on the lines that match or do not, `:m` and `:t`
  to move and copy lines, `:pu`, and `&` and `g&` to repeat the last
  `:s`. `:g` and `:normal` undo as one step.
- Vim mode adds `it` and `at` for the text in tags, `[( [{ ]) ]}` to go
  to the bracket that holds the cursor, and `gp` and `gP`.
- In vim mode a count before an insert types its text as many times, as
  `3ix` and `2o` do, and `.` repeats it so; `r` and Enter leaves the
  cursor where vim does.
- In vim mode, ⌃W and ⌃U delete the word and the line before the caret
  in insert mode, on macOS.
- In vim mode `ma` sets a mark, where before `m` was dropped and `a`
  started inserting.
- Vim mode's search highlights draw from what the last frame found
  while nothing changed, rather than searching the lines in view every
  frame.
- In vim mode the cursor is half a block while an operator, as `d`,
  waits for its motion, and the text yanked lights up for a moment.
- Vim mode searches: `/` and `?` open a command line over the bottom
  of the editor, which moves to the first match as you type, highlights
  every one and counts them; `n`, `N`, `*` and `#` search again, a
  search is a motion as in `d/end`, and the status bar says when a
  search went round the end or found nothing. Patterns are vim's.
- Vim mode's `:` runs commands: `:w`, `:q`, `:wq`, `:x`, a line number,
  `:s` and `:%s` over a range or the selection, `:d`, `:y`, `:j`,
  `:sort`, `:noh`, `:e`, `:vs` and more, with ↑ and ↓ for the commands
  typed before.
- Vim mode adds `gv`, `gi`, `gJ`, `H M L`, `zz zt zb`, ⌃A and ⌃X to add
  to a number, and `ZZ` and `ZQ`.
- Vim mode does as vim does in more places: `2D` and `d}` take whole
  lines rather than leave a blank one, `$` in visual mode takes the end
  of the line, `di(` and `ci[` find the next brackets along the line
  when the cursor is outside any, `da"` takes the space before the
  quotes when none follows, `di{` over lines leaves the cursor on the
  closing brace, and a paste over lines leaves it on their first.
- Vim mode's `ip` and `ap` take a paragraph, `r` and Enter breaks the
  line, `3V` and `3v` select as many lines or characters, and `.`
  repeats a change made in visual mode, as `Vj>`, over as much text.
- In vim mode, `p` over a selection yanks what it replaces, as vim does,
  and `P` keeps the register; with nothing to paste, the selection goes.
- Esc after `o`, `O`, `S` or `cc` with nothing typed takes back the
  indent, rather than leave a line of spaces.
- Vim mode's keys work on keyboards of other scripts, as Cyrillic, from
  where a US keyboard has them.
- Find, quick open and the command palette shake their heads when
  Return finds nothing to go to, as a Mac's password field does.
- The find box goes back up as it fades out when closed, as do the peek
  of a change and the box of a review comment.
- The fields of find and search fade their accent border in and out as
  they take and leave the keyboard.
- The rows of quick open, the command palette and the pickers, the
  buttons of Source Control, its commit button and branch, the buttons
  of diffs and reviews, and the sync panel fade their hovers rather than
  blink, and their small buttons sink as they are pressed.
- The buttons that show only under the pointer, as those of a group of
  changes or the Explorer's folder row, fade in and out.
- The line along a splitter shows once the pointer rests on it, and
  fades, rather than flashing as the pointer crosses it; a drag shows it
  at once.
- The badges of an empty folder, of no folder, of a folder with no
  repository and of No Changes pop in, so a commit that leaves nothing
  to commit ends with a check.
- Switching between Explorer, Search and Source Control fades the view
  in.
- The counts of Source Control's groups roll to their new numbers.
- Tabs move: a new tab grows in, a closed one folds away while the
  others slide over, the line over the tab shown slides to the next one,
  the cross fades in under the pointer, and the dot of unsaved changes
  pops in. Terminal chips in the panel grow and fold the same way.
- Buttons, tabs, breadcrumbs, the status bar and the welcome page fade
  their hover colors in and out rather than blink, and icon buttons sink
  a little as they are pressed.
- The mark under the sidebar's view switches slides to the view chosen,
  and the mark of the row chosen in quick open, the command palette and
  the pickers slides as the arrow keys move it.
- Chevrons turn as their sections, folders and the replace fields open
  and close, rather than swap.
- Counts roll to their new numbers: the changes on the Source Control
  switch, and find's match count, down when going back.
- The Vim badge fades between its modes' colors, and the name of the
  branch, the preview's icon and the name in the title bar cross-fade as
  they change.
- The welcome page's logo, Start and Recent rise in one after another.
- Going back to a wrapped file's tab no longer jumps it up the file at
  times; it shows where it was left.
- The box in the middle of the title bar no longer leaves a gap after a
  short name, as a terminal's `zsh`, before the folder.
- The welcome page's rows are all one height, with keys or without, so
  Start and Recent keep one rhythm.
- In Vim mode, the status bar shows the mode as a badge, colored by
  mode: plain in normal mode, the accent color in insert mode, and amber in visual
  mode, in place of `-- NORMAL --`.
- The command palette and the welcome page's Command Palette… row show a
  ⌘ icon instead of a chevron, and the welcome page's logo lines up with
  the icons of the rows below it.
- With more than six recent folders, the welcome page ends its list with
  More…, which opens Open Recent (⌃R) to show the rest.
- Source Control's message box starts one line high and grows with the
  message, up to eight lines; a hint that wraps in a narrow sidebar shows
  whole.
- Cut with no selection cuts the caret's entire line,
  and can be undone in one step.
- Splitting a diff to the right no longer closes the window (#20, #39).
- Quick open and the command palette match a run of letters together:
  `git` lights up the whole of **Git** in "Git: Fetch", rather than its
  last t, and such names rank higher.
- The Markdown preview draws a line under the largest headings and mutes
  quotes, as GitHub does.
- The welcome page gives Quick Open an icon of its own, apart from Find in
  Files.
- **Breadcrumb folders:** click a folder in the breadcrumbs to browse its
  files in a tree below it, without the sidebar. Its folders open in
  place, and the file you choose shows in that column; the arrows, Enter
  and Esc work too. It works for files outside the folder and on SSH
  hosts, and leaves the Explorer as it was (#38).
- In light themes, the names of changed, added and deleted files, and
  error messages, are drawn darker so they meet WCAG AA contrast (4.5:1);
  the gutter marks keep their brighter colors.
- A `fontFamily` that isn't installed falls back to the system's
  monospaced font, not to its proportional one; so does
  `terminalFontFamily`.
- The breadcrumbs of a file outside the folder no longer start with an
  empty segment, and show the home folder as `~`.
- Typing in a long file highlights with less work: only the lines near the
  edit go to the lexer, not the rest of the file, and a keystroke within a
  line updates the colors in place rather than copying those of every line.
- **Submodules in Source Control:** each initialized submodule, nested ones
  and those of linked worktrees too, lists its own changes to stage and
  commit. A picker in the commits header and the status bar shows another
  repository's history, or, on Auto, the one of the active file.
- **AI commit messages:** signed in to your mon.sorrycc.dev account, the ✨
  button at the corner of Source Control's message box writes the commit
  message from the staged changes, or from every change when nothing is
  staged, in the style of the last commits. It streams into the box, and a
  second click stops it; text already in the box goes along as notes.
  **Git: Write Commit Message with AI** in the command palette does the same,
  and the `aiModel` setting picks another of Mon's models.
- **Mayoi → Sign In…** is the new name of **Sign In to Sync Settings…**, as
  the account now does more than sync.

## 0.10.0

- **Settings sync:** **Mayoi → Sign In to Sync Settings…** (**Help** on
  Windows and Linux) signs in to a mon.sorrycc.dev account with GitHub or
  Google, and syncs `settings.json` and `keybindings.json` between your
  machines. Changes merge setting by setting; `keybindings.json` changed on
  two machines keeps this one's and saves the other's beside it.
- **Settings sync** shows how it's going: a cloud at the right of the
  status bar opens the account's panel, with your name and picture, when
  the machine last synced, **Sync Now**, and **Manage Account** and **Sign
  Out**. The sign-in dialog walks through the steps, shows the code in large
  tiles and counts down until it expires. The welcome page offers to sign
  in.
- Settings sync keeps what you choose on one machine: switches in the
  account's panel, or `syncIgnored` in `settings.json`, as
  `["fontSize", "keybindings.json"]`.
- Settings sync tries again by itself when it can't reach the account, says
  why in plain words, and saves a change made just before you quit.
- `keybindings.json` changed on two machines opens side by side with the
  other's to merge, from the toast or the account's panel, or **Keep Mine**
  drops the other's. **Sign Out of Settings Sync…** asks first.
- Mayoi speaks Simplified Chinese and Japanese as well as English (#33):
  the menus, the command palette, the panels, the dialogs and the messages
  follow the system's languages, and **View → Language**, the command
  palette's **Language** commands and the `locale` setting choose one. The
  command palette still finds commands by their English names.
- **Compare HEAD to Here**, **Compare Here to HEAD** and **Compare with
  Selected** open the changes in a diff tab, inline or side by side, with
  changed words tinted and unchanged lines folded, rather than as a patch
  in a text tab; a card names the two commits, and the tab comes back with
  the window.
- The terminal panel's header has a menu: right-click it to move the panel
  to the bottom, left or right, maximize it or hide it.
- Source Control's **Remotes** shows each remote with a cloud and where it
  points, as `github.com/owner/repo`, before you expand it.
- Switching Source Control's **Repository** back to **Remotes** no longer
  shows "No remotes" for a moment while it reads them again.
- A commit in Source Control keeps its time in view in a narrow sidebar or
  under an expanded branch: its branch and tag badges narrow, or count the
  rest, before they push the time out.
- The terminal panel can sit at the bottom, left or right of the window:
  **View → Panel Position**, the command palette's **Move Panel to Left**,
  **Right** and **Bottom**, the `panelPosition` setting and
  `keybindings.json`'s `workbench.action.positionPanelLeft` and its
  neighbours choose where, and a panel still starts at the bottom. At a side
  it sits past the sidebar, so with both on a side the panel is the outer
  one, and dragging its inner edge sizes it. The title bar's panel button is
  a terminal now, and it follows the panel's side: at the left of the bar
  when the panel is there, past the sidebar's button when the sidebar is
  there too, at the right otherwise.
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
- A diff no longer crashes Mayoi when files leave it, as when changes
  are committed or discarded, while it is scrolled down to them.
- With **View → Project Tabs** on, the rail of project tabs shows while
  a window has one project too, rather than only once it has two, so the
  project shown and the button that opens another folder stay in view.
- The rail always shows each project's favicon or logo, keeping the
  initials for a project without one; **View → Project Icons** and the
  `projectIcons` setting are gone.
- A remote folder's square on the rail has a server badge in its corner,
  colored by how its host is connected, and its tip says so.
- The rail labels each project with two letters, telling apart those that
  start alike: Downloads and Documents show as "Dw" and "Dc" rather than
  both "D".
- A square's context menu has **Rename Tab…**, which shows up to two
  characters of your own, an emoji too, in place of its initials or logo,
  and **Tab Color**. They're kept in `projectTabStyle` in `settings.json`,
  on each machine.
- Drop a picture on a project's square, from the Finder or the Explorer,
  or pick one with **Choose Icon…** in its context menu, and the square
  shows it in place of the project's logo: a PNG, JPEG, WebP, SVG, ICO or
  ICNS, copied into `project-icons` beside `settings.json`. **Reset Icon**
  brings the logo back, and a label given with **Rename Tab…** takes the
  icon's place.

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
