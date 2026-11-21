# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## Unreleased

- Opening a file to the side from Quick Open with `Cmd+Return` uses an existing column instead of adding one.
- **Layout → One File at a Time** no longer applies to a commit's changes, which list every file.
- `Cmd+Q` and **Mayoi → Quit Mayoi** ask before quitting, with **Don't ask again** to skip it.
- **Settings → General → Confirm Before Quit** turns the quit question back on.

## 0.23.1

- Set the TypeScript plugin's `path` to `tsrs` to use tsrs.
- Set the Python plugin's `path` to `ty` to use ty.
- Closing the settings window keeps what was typed in a text field, rather than losing it.
- Archive sessions from a row's menu, its archive button or `⌫`, and **Undo** from the toast.
- Choose several sessions to archive them together.
- The archive button in the Sessions title, or **View → Archived Sessions**, lists archived sessions to restore.
- Resuming an archived session brings it back to the list; its terminal and transcript are left alone.
- Give a language server your own startup options with `languageServers` in settings.json, by the server's id.
- Options go over the plugin's own, deeply, and `null` takes one away, as in `{"tsserver": {"path": null}}`.
- Saving changed options restarts only the servers they're for, in every folder.
- A language server plugin's details show where to set its `initializationOptions`.
- Holding `Cmd` (`Ctrl` on Windows and Linux) over a name shows a hand, as a click goes to its definition.
- Drag a file or folder onto a folder in the Explorer to move it there; open tabs follow it.
- On Windows a new terminal is titled by its shell, as `pwsh`, rather than the shell's full path.
- The **Go** menu finds type definitions, source definitions, implementations and references.
- Language server plugins can start a bundled runtime on this machine.

## 0.23.0

- Type at several places at once: **Selection → Add Cursor Above** and **Below** add a caret on the next line.
- Alt-click (`⌥`-click on macOS) adds a caret or takes one away; `⌘`-click still goes to a definition.
- `⌘D` selects the next match of the word or selection, and `⇧⌘L` selects every match at once.
- Drag with `Shift+Alt` or the middle button to select a column; `⇧⌥⌘` with the arrows does it from the keyboard.
- Typing, delete, Enter, Tab, copy, cut, paste and the arrows act at every caret, and one undo takes an edit back.
- The status bar counts the selections, and Escape or a plain click goes back to one caret.
- **Go → Show Hover** now opens a small card by the word at the caret instead of a dialog in mid-window.
- The hover card stays inside its editor and split, above the line or below it near the top.
- A hover's signature shows first in the code font and colors, with its documentation below.
- Plaintext hovers from language servers show as written, without turning `*` or `_` into formatting.
- The hover closes on `Esc`, a click elsewhere, typing, or moving the caret; long hovers scroll.
- **Go → Show Hover** has a shortcut: `⌘K ⌘I` on macOS, `Ctrl+K Ctrl+I` on Windows and Linux.
- Pressing Show Hover again while the card shows lets the arrows and `Page Up`/`Page Down` scroll it.
- Rest the pointer on a name in code to see its hover card; move into the card to read or scroll it.
- Turn off the pointer's hover cards with the `editorHover` setting.
- In Vim mode, `gd` goes to the definition and `K` shows the hover.
- A late definition or hover no longer replaces Quick Open or a list you opened while waiting.
- Lists in hovers and plugin views now show their bullets and numbers.
- A language server that refuses to start now shows its own reason instead of "the server stopped".
- The TypeScript server says when a folder has no TypeScript, and how to add it or point to one.
- With TypeScript 7, set the TypeScript plugin's `path` to `tsc` to use its own server.
- The TypeScript plugin's `tsserverPath` setting names a `tsserver.js` when the folder has none.
- Mayoi no longer crashes when a language server exits without an error as it starts; it says so.
- Mayoi comes with language servers for Go, TypeScript, Python, Rust and C/C++, off until you turn them on.
- Opening a file whose built-in server is off offers **Turn On** when its program is installed.
- If a built-in server's program is missing, Mayoi says how to install it.
- Each built-in server has a `path` setting on the **Plugins** page for a program that isn't on your PATH.
- A plugin you install under the name of a built-in one now takes its place.
- C++, JSX and TSX files now reach language servers as `cpp`, `javascriptreact` and `typescriptreact`.
- Files dropped from the Finder on a project's square switch to that project and open there as tabs.
- Files dropped on the empty part of the project rail open in the project shown.
- Pictures dropped from the Finder on a square open as files; use **Choose Icon…** to set an icon.
- Splitting a Live Preview note with tables, diagrams or formulas into two columns no longer crashes Mayoi.
- Fold blocks of code: click the chevron beside a line number, shown as the pointer comes over the gutter.
- JSON objects and arrays fold, as do Go, JavaScript and TypeScript blocks, Python by indent and Markdown sections.
- A folded line ends in `⋯`; click it, or its chevron, to show the lines again.
- On macOS, **Edit → Fold** and **Unfold** use `⌥⌘[` and `⌥⌘]`, which also fold a focused diff's files.
- **Edit → Fold All** (`⌘K ⌘0`) and **Unfold All** (`⌘K ⌘J`) fold or open every block of the file at once.
- The caret moves past folded lines; Find and Go to Line open the folds that hide what they go to.
- Folds stay as you edit above them, each split view keeps its own, and they come back with your session.
- ⌘-click a name, or Ctrl-click on Windows and Linux, to go to its definition, as `F12` does.
- **Go → Back** (`⌃-`) and **Go → Forward** (`⌃⇧-`) return to where a jump, as to a definition, left.
- On Windows and Linux, `Alt+←` and `Alt+→` go back and forward; the mouse's side buttons do too.
- Go back lands where you were even after edits above it, and reopens a file you closed since.
- A name defined in several places offers them in a list instead of going to the first.
- Go to Definition ignores an answer that arrives after you moved the caret or asked again.
- Definitions on the same line each show in the list, with their column so you can tell them apart.
- A language server restarted while asked for a definition no longer says none was found.
- **Go → Show Hover** ignores an answer that arrives after you moved the caret, typed or asked again.
- **Selection → Complete Word** no longer opens a late list or says "No completions here" once you've moved on.
- An older completion list no longer replaces or closes the one you asked for since.
- A language server restarted while asked for a hover or completions shows nothing from the old one.
- Show Hover and Complete Word say when the language server isn't running instead of finding nothing.
- Go to Definition uses the text as you just typed it, and stays put if you moved on before it answered.
- Opening a file whose language has no server offers its plugin, once, when the server is installed.
- When that server isn't installed, Mayoi says once how to install it.
- Once you install a server Mayoi said was missing, opening a file of its language offers its plugin.
- A plugin that fails to install from that offer says why and offers **Try Again**.
- The installed plugin's server starts at once for the files already open.
- **Plugins → Show Language Servers** lists this folder's servers, whether each runs, and where.
- Choose a server there to restart it, even after Mayoi stopped retrying it on its own.
- A server that can't start says why: its program isn't installed, it quit, or it didn't answer.
- On an SSH host, a server ending with status 127 is only called not installed once the host confirms it.
- A server that fails to start or stops offers **Restart**; files already open reconnect to it.
- Terminals no longer crash Mayoi on macOS while many files are open.
- Selecting text in a file with invalid UTF-8 past its first 8 KB no longer crashes Mayoi.
- **Settings → Terminal → Shell** picks the program new terminals run, as PowerShell 7 Preview or Git Bash.
- **Shell Arguments** passes each argument to that program exactly as written, never through a shell.
- A click past bold text or hidden Markdown marks puts the caret where you click, without selecting text.
- **Toggle Line Comment** works in JSX, Emacs Lisp and GDScript, and a typed backtick closes in JSX too.
- Turn on `trimTrailingWhitespace` to remove the spaces and tabs at the ends of lines as you save.
- Trimming on save keeps Markdown's two-space line breaks and comes back with one **Undo**.
- **Trim Trailing Whitespace** in the command palette trims the open file now.
- Find and replace take a selection of several lines as the query, and highlight every line of a match.
- The find and replace fields grow with the lines of their text, the buttons beside them staying put.
- Heading crumbs show just the heading's name, without its `#` marks, and the heading you're in stands out.
- A deep chain of headings in the breadcrumbs shows the outermost and innermost two, with `…` for the rest.
- A heading crumb you move into opens in smoothly, and the crumbs after it slide along.
- The breadcrumbs' heading outline tags each heading with its level, as `H2`, and marks the one you're in.
- Type in the breadcrumbs' heading outline to filter it; `↑`/`↓` pick a match and `Enter` goes there.
- The TODOs plugin finds tags with symbols or non-Latin letters, such as `[TODO]`, `@todo` or `待办`.
- With **Layout → One File at a Time**, a diff shows the whole file; **Show Full Files** folds it.
- `Cmd+S` keeps a preview tab open, even with no changes, so the next file you click opens beside it.
- Right-click a tab and choose **Pin Tab** to keep it at the left of the tab bar, marked with a pin.
- **Close Others** and **Close All** leave pinned tabs open; **Unpin Tab** in the same menu releases one.
- SVG previews show the text in the picture, Chinese labels included, in its fonts, sizes and colors.
- `.jsonl` and `.ndjson` files open as a table, a row per line and a column per key; `⇧⌘V` shows the source.
- **View → Open JSON Lines As** chooses whether JSON Lines files open as their table or their source.
- The table shows the chosen row whole below it, indented and colored; `Space` hides or shows it.
- Type in **Filter rows**, or press `⌘F`, to keep only the rows that hold the text, across the whole file.
- Click a column's header to sort by it: numbers by value, text as people read it, versions included.
- Right-click a header to hide, show or sort a column, or expand an object or array into its own columns.
- Lines that aren't JSON stay in the table in red; the status bar counts them and a click goes to the next.
- `Enter` or a double-click on a row shows that line in the source; `⌘C` copies the cell or the whole line.
- The JSON Lines table marks what **Filter rows** matched, in its cells and in the row's details.
- A row's details name the field of the chosen column and mark it in the indented JSON.
- The chosen row's line number stands out in the table, as the caret's line does in the editor.
- The details' copy button turns to a check for a moment after it copies the line.
- A JSON Lines row's details scroll to the field of the chosen column, and start at the top for each row.
- `⌘C` in a JSON Lines table flashes the cell or row it copied, and copying a line checks the details' copy button.
- Right-click a JSON Lines cell and choose **Filter to This Value** or **Hide Rows with This Value**.
- Value filters show as chips beside **Filter rows**; click a chip's `×` to remove it, or press `Esc` to clear all.
- When no row matches, the table offers **Clear Filters** to show every row again.
- JSON Lines files with records over 64 KB open as their table again; their source names the long line.

## 0.22.4

- Clicking a link in Live Preview to a note already open in that column switches to its tab, not another column.
- Double-click the empty tab bar, past the last tab, to open a new untitled file in that column.
- The Markdown Reading view shows an HTML `<table>` as a table, with its cells' alignment and pictures.
- HTML `<ul>`, `<ol>` and `<li>` show as lists in the Reading view, nested and numbered.
- A `<details>` folds under its `<summary>` in the Reading view; click the summary to open it.
- `<sup>` and `<sub>` show raised and lowered, `<u>` and `<ins>` underlined, `<small>` smaller.
- `align="right"` aligns HTML text to the right in the Reading view.
- A `<picture>` shows its dark-theme image in the dark theme, as a README's logo does on GitHub.
- An HTML `<pre>` shows as a code block with its spaces kept; a `<blockquote>` shows as a quote.
- The link icon in centered text, as a README's centered links, now stands right after its link.
- Scrolling past a long wrapped line of Chinese or other non-ASCII text is smooth again.
- In Live Preview, a file path in the text, like `~/notes/a.md` or `./a.md:12`, is a link that opens it.
- On Windows, a path like `C:\notes\a.md` or `.\a.md` in Live Preview is a link too.
- Markdown links to `~/` paths and whole paths, like `[a](~/a.md)`, open their file in Live Preview.
- In a Markdown file, the breadcrumbs show the headings you're in after its name, as `# Title > ## Section`.
- In the Reading view, the breadcrumbs name the heading at the top of what shows, and follow as you scroll.
- Click a heading in the breadcrumbs for an outline of all the note's headings; click one to go to it.
- A folder's box in the breadcrumbs now fits all its rows instead of cutting off the last one.

## 0.22.3

- Codex subagents no longer appear as duplicate entries in **Sessions**.
- On Windows, **File → Remote SSH** opens a folder with the system `ssh`.
- On Windows, **File → Open WSL Folder…** opens a folder in a WSL distribution.
- On Windows, a Remote SSH or WSL file keeps a `/ssh:` or `/wsl:` path and opens in one tab.
- On Windows, adding or removing a file at the root of a remote folder updates Explorer.
- On Windows, a nested repository's changes tab stays with that repository after a restart.
- On Windows, Source Control shows a remote file's folder with slashes.
- A diff can show one file at a time: **Layout → One File at a Time**, or the `diffFiles` setting.
- With one file at a time, clicking a file in Source Control or a commit's files shows that file.
- With one file at a time, the next change or next file goes on to the next file, as `diffCrossFiles` says.
- With one file at a time, the diff's tab is named after the file it shows.
- Mayoi asks before installing plugins that settings sync brings from another machine.
- Settings sync installs plugins only from `https://`, `ssh://` or `git@` URLs.
- Hovering an inline blame cut off at the edge of the editor shows all of it in a tip.
- **View → Toggle Preview** and the other preview commands show an unsaved new file as Markdown.
- A preview command on a file that has no preview says so instead of doing nothing.

## 0.22.2

- Scrolling over a Mermaid diagram in Live Preview scrolls the text, and clicking it edits its source.
- Windows Security is less likely to flag the Mayoi installer as a virus.
- The Windows installer shows Mayoi's name, version and publisher in its file properties.
- List numbers and bullets in issue and pull request comments line up with the first line of their item.
- On macOS 14, the terminal now opens and shows its prompt instead of closing at once.
- Opened from the Finder or the Dock, Mayoi pushes over SSH with your shell's ssh agent.
- Git operations go through the proxy your shell sets in `HTTPS_PROXY`, `ALL_PROXY` and the like.
- Git hooks find `node` and `pnpm` even when iTerm2's shell integration is installed.
- A session open in a terminal shows a chip saying where: **In tab**, or the other window's project.
- A pull request opens in one tab with **Conversation**, **Changes**, **Commits** and **Checks** pages.
- Switch a pull request's pages with `⌥⌘1`–`⌥⌘4`; the tab remembers the page it showed.
- A pull request's **Conversation** shows whether it can merge, its reviewers, reviews and comments.
- **Review** approves a pull request, requests changes or comments, from its tab.
- **Merge** squashes, rebases or makes a merge commit, and can delete the branch on GitHub.
- A pull request's **⋯** menu marks it ready or a draft, closes or reopens it, and copies its branch.
- Review threads show under their lines in a pull request's changes, to reply to, resolve or reopen.
- Failed checks run again with **Rerun Failed Jobs** from a pull request's tab.
- The status bar's pull request opens its changes; its checks icon opens the **Checks** page.

## 0.22.1

- Mayoi no longer stops responding about 20 seconds after it opens or after the Mac wakes from sleep.

## 0.22.0

- The Matrix and Claude themes use the same interface font as the other themes, not the code's font.
- **View → Theme** adds GTA VI: the game's HUD in graphite, white and mission gold, always dark.
- In GTA VI, wanted stars light up in the status bar as uncommitted files pile up, and flash while Git works.
- On Windows, right-click a file, a folder or a folder's background in Explorer and choose **Open with Mayoi**.
- The Windows installer adds **Open with Mayoi**; on Windows 11 it is under **Show more options**.
- **File → Add 'Open with Mayoi' to Explorer** and **Remove…** add or remove it without reinstalling.
- Files in GBK, Big5, Shift JIS or UTF-16, such as CSVs from Excel, open as text instead of as binary.
- Such files save back in the encoding they were read in, and the status bar shows it, as `GB18030`.
- Drag to select and copy the text of an issue or pull request's description and comments.
- Select several items in the GitHub view with `⌘`-click, `Shift`-click or `Shift` with the arrow keys.
- `⌘C` and **Copy Links** in the GitHub view copy the links of every selected issue and pull request.
- Drag issues and pull requests from the GitHub view into a terminal to type their links.
- Drop issues and pull requests from the GitHub view on an editor to open them in tabs.
- `mayoi plugin search` says so when the index of plugins lists none yet, instead of printing nothing.
- The help of `mayoi plugin` no longer shows an install example for a plugin that does not exist.

- Closing a terminal while its agent is working or waiting for you now asks first, as `⌘W` does for changes.
- The buttons on the selected row of the CI checks and stash lists respond to clicks again.
- A GitHub view in the sidebar lists the open issues and pull requests of a GitHub repository, via `gh`.
- The GitHub view puts reviews asked of you, your pull requests and your issues first, and counts reviews.
- Type in the GitHub view to find an issue or pull request; `⌘C` copies the link of the one chosen.
- `⌘C` copies the chosen item's link while you type in the GitHub view, and a list's link on "more on GitHub".
- The GitHub view highlights the issue or pull request whose tab is shown, as you switch tabs.
- Double-click an issue or pull request in the GitHub view to open it in a tab that stays, not a preview.
- Without `gh`, signed out, or with several repositories as remotes, the GitHub view says what to do next.
- Issues open in tabs with their labels, comments and pictures; comment, close or reopen them there.
- **New Issue**, in the GitHub view or **Git → New Issue…**, writes and creates an issue in a tab.
- **Work on Issue** makes a worktree for an issue and hands it to the agent you start in its terminal.
- **Address Review Comments** shows a pull request's open review threads as notes to send to an agent.
- Pull requests open in a tab right away and load inside it, with any error and **Try Again** shown there.
- Clicking a pull request in the GitHub view opens a preview tab, as clicking an issue does.
- A review note from GitHub has **Resolve on GitHub**, which resolves its thread there.
- A pull request's tab has **Open Conversation** to read its description and comments.
- In the GitHub view, labels that don't fit are left out whole instead of cut off, and an item's age always shows.
- GitHub view rows mark pull requests approved or with changes requested in color, and leave out your own name.
- The GitHub view's sections have room between them, and their headers darken under the pointer.
- Rows of a section you open, or that a refresh brings, fade in instead of popping in the GitHub view.
- While the GitHub view first loads, its placeholder has the list's shape, so nothing jumps when rows arrive.
- Hover a GitHub view row for **Check Out** or **Work on Issue**, **Open on GitHub** and **Copy Link** buttons.
- Rest the pointer on a GitHub view row for a card with its full title, all labels, assignees and branch.
- The GitHub view's card shows a pull request's size, each check that passed, failed or runs, and its review.
- When GitHub can't be read again or you're offline, the GitHub view says so at its foot without moving the list.
- A GitHub view row under the pointer is no longer darker than other hovered rows in the sidebar.
- In the GitHub view, the selected row's highlight glides to the next row within a section, as in Sessions.
- A pull request's check icon in the GitHub view fades to its new state when a refresh shows checks finishing.
- `Space` shows or hides the selected GitHub view row's card; **Show Details** in its menu does too.
- `⌘Enter` checks out the selected pull request or works on the selected issue; `⌥Enter` opens it on GitHub.
- With the GitHub view's list focused, the selected row shows its buttons, and their tooltips name their keys.
- Commit cards in the Source Control history open toward the editor when the sidebar is on the right.
- GitHub view rows show the author's picture, with their initials until it arrives.
- The GitHub view's card shows pictures of the author and assignees.
- A pull request's card lists each reviewer: approved, changes requested, commented or pending.
- A pull request's title in the GitHub view no longer shifts when its checks first appear.
- Arrow keys in the GitHub view skip notes such as "No issues" that have nothing to open.
- Pull request rows in the GitHub view show up to three reviewers' pictures, with a count of the rest.
- A pull request that asks for your review again says **Re-review** in its GitHub view row and card.
- The GitHub view loads on repositories with many open pull requests instead of timing out.
- When GitHub doesn't answer in time, the GitHub view keeps what it showed and tries again within a minute.
- While checks run, a pull request's row and card show a turning ring, held still by Reduce Motion.
- Issue and pull request tabs show pictures of the author, assignees and everyone who commented.
- The GitHub view refreshes quietly on its own; only **Refresh** or **Retry** shows a progress line, and only if slow.
- The GitHub view's Refresh button turns into a spinning ring until GitHub answers.
- You can type in the GitHub view's search field while it first loads; its placeholder rows pulse meanwhile.
- A GitHub view refresh that finds nothing new no longer redraws the list.
- In the GitHub view, rows a refresh removes, such as a merged pull request, fade out instead of vanishing.
- Folding or unfolding a GitHub view section slides its rows closed or open.
- The GitHub view keeps the selected row on the same issue or pull request when a refresh adds or removes rows.
- A GitHub search that finds nothing offers **Clear Search**.
- **Try Again** in the GitHub view shows a spinning ring as soon as you click it, until GitHub answers.
- Labels in an issue's tab use the same chips as the GitHub view, and a failed read offers **Try Again** on a card.
- Draft pull requests in the GitHub view have a draft icon, a dimmer title and a grey **Draft** chip.
- GitHub view rows show Approved, Changes Requested and Re-review as filled chips that stand apart from labels.
- Press `Escape` in the GitHub view's search field to clear it.
- Moving the selection to another GitHub view section fades it in instead of jumping, and it glides past rows that slide.
- When the top GitHub view section goes away, the space above the next section closes smoothly instead of jumping.
- In the GitHub view, the warning icon shakes when **Try Again** fails again, so you know it tried.
- In a narrow sidebar, GitHub view rows hide reviewer pictures and comment counts before squeezing the author's name.
- The Re-review, Approved and Draft chips in GitHub view rows stay whole in a narrow sidebar.
- Buttons on the GitHub view's cards press in when clicked, like the rest of the sidebar's buttons.
- GitHub view section headers now use the same type size as the day headers in Sessions.
- Mayoi sends an anonymous count once a day and on each update, with only its version and system.
- Turn the count off with `usageStats` in **Settings → General**, or by setting `DO_NOT_TRACK=1`.

## 0.21.2

- The scroll bar no longer jumps while you scroll notes with wrapped lines, in Live Preview and in the editor.

## 0.21.1

- `[[` suggestions list notes changed most recently first, after the ones you opened recently.
- Pushed commits in the Source Control history show a ✓, ✗ or clock for how their GitHub CI checks went.
- Clicking a commit's CI icon, or **Show Checks…** in its menu, lists its checks with logs and re-run.
- A commit's tab shows how many of its checks passed, failed or are running.
- The status bar shows HEAD's CI checks when the branch has no pull request.
- When CI on HEAD finishes, Mayoi tells you, with a system notification if it is in the background.
- Windows directory junctions show as folders in the file tree, and copy with what they hold.
- Commits made with an AI agent, like Claude, show the agent's mark on the author's avatar in the history.
- `⌘C` in the sessions list copies the chosen sessions' transcript files, like dragging them.
- Hovering a commit in the history shows a card with its author, co-authors, files changed and branch.
- A commit's tab names its co-authors beside the author, in place of the `Co-authored-by` lines.
- Clicking a session of another project in **All Projects** chooses it in that project's Sessions view.

## 0.21.0

- Typing `[[` in a note suggests the vault's notes and files as you type, as in Obsidian.
- In `[[` suggestions, `↑`/`↓` choose, `Enter` or `Tab` links, `Shift-Enter` links as typed, `Esc` closes.
- After `#` the suggestions are the note's headings, and after `^` its blocks, giving a block an id.
- `[[#` and `[[^` suggest the headings and blocks of the note itself.
- Chosen links follow the vault's `newLinkFormat`, the shortest path by default, without `.md`.
- `Ctrl+Space` inside a `[[link]]` shows its suggestions again.
- `[[links]]` also find notes in hidden folders like `.claude`, but not `.obsidian` or `.trash`.
- `[[links]]` find notes anywhere in an Obsidian vault, even in a folder with its own git repository.
- `[[links]]` in notes on a remote (SSH) folder find notes anywhere in the workspace, not only beside the note.
- `[[links]]` match notes whatever their case, and pick up notes made or deleted outside the editor.
- Live Preview stays smooth in notes with many `[[links]]` and in large vaults.
- In Live Preview, clicking a link opens it, as in Obsidian; move the caret into a link to edit it.
- Clicking a `[[link]]` to a note that doesn't exist yet creates the note beside the current one and opens it.
- `[[note#^block]]` links open the note at the marked block.
- `[[note#Heading#Subheading]]` links open the note at the heading under the other heading.
- A key binding passes its `args` to commands that take one, as `file.openFolder` with `"~/Projects/notes"`.
- **Sessions** lists Pi's sessions too, by name or first prompt, and resumes one in a terminal tab.
- The sidebar's width and whether it's shown now stay the same as you switch projects and windows.
- **Sessions** keeps **All Projects** chosen as you switch projects.
- With **All Projects** chosen, **Sessions** stays open in the sidebar as you switch projects.
- Projects on a remote (SSH) folder no longer show **Sessions**, which listed this Mac's sessions.
- **Sessions** marks a session that's open in a terminal in any project with a terminal icon.
- Clicking such a session switches to its project and shows that terminal.
- Opening local files keeps the window responsive while their tabs show loading progress.
- A file on a network share that stops answering no longer keeps other files from opening.
- Files that fail to open keep their tabs, error details and a **Try Again** action.
- Quick Open stays responsive while checking file paths, including unavailable network shares.
- Filtering Quick Open, symbols or commands keeps the selected result in view.
- A folder holding many Git repositories no longer makes the window sluggish.
- Typing a bracket or quote that closes itself no longer scrolls or shakes the editor.
- In Markdown, `Enter` continues a list, task list or quote, and on an empty item ends it.
- In Markdown, `⌘Enter` switches a line between a list item, an empty task and a checked task.
- In Markdown, `⌘⇧↑` and `⌘⇧↓` move a list item and its sub-items past the item above or below.
- **Settings ▸ Shortcuts** lists `⌘Enter` for **Toggle Task**, not **Insert Line Below**.
- In Markdown, `⇧⌘Enter` opens a new item of the list or quote above.
- In Markdown, `Tab` and `⇧Tab` indent or outdent a whole list item wherever the caret is.
- **Edit ▸ Markdown** toggles tasks, lists, quotes, headings and embeds, and clears a line's formatting.
- `⌥⌘1` to `⌥⌘6` toggle a Markdown heading of that level on macOS.
- **Selection ▸ Join Lines** joins the selected lines, or the caret's line with the next, into one.
- **Sessions** shows what Qoder and Codex sessions do, as it does for Claude Code sessions.
- **Sessions** shows what sessions running in Mayoi's own terminals do, without `terminalBackend` set to `"hc"`.
- New Live Preview edits Markdown as it reads: headings, emphasis, links and code styled, their marks hidden.
- In Live Preview, the marks of what the caret touches show, faint, fading in as the caret reaches their line.
- Live Preview draws bullets, task boxes, quotes, alerts, pictures and math in place, as the Reading view does.
- Live Preview renders tables, Mermaid diagrams and display math, and shows their source when the caret goes in.
- In Live Preview, text below a table, diagram or formula glides as its source shows or hides.
- Bullets, task boxes and list indents in Live Preview keep their width as the caret reaches them.
- Nested lists and lines that continue a list item stand under the item's text in Live Preview.
- Quotes in Live Preview have one unbroken bar, and the largest headings a hairline below, as in Reading.
- A click on a task's box in Live Preview checks it, filling the box and striking its text through.
- A click on a task's box in the Reading view checks it in the file.
- A picture from the web in Live Preview shows a card in its place while it loads, then fades in.
- Symbols such as `⌘` and wide characters keep a table's columns lined up in its Live Preview source.
- Headings in Live Preview have room above them, and their lines stay put as the caret reaches them.
- Footnote definitions like `[^1]: text` show as their number in Live Preview until the caret reaches them.
- Links to web pages in Reading view end with the same external-link icon as in Live Preview.
- Embedded notes like `![[note]]` now show in place inside list items and quotes, in both views.
- Holding `⌘` over a link in Live Preview shows a pointing hand at once, and letting go hides it.
- Buttons at the right of a Markdown tab's path switch it between source, Live Preview and the Reading view.
- Switching between source, Live Preview and the Reading view fades the new view in.
- **View → Toggle Live Preview** (`⌥⌘V`) switches a Markdown tab between Live Preview and its source.
- **View → Open Markdown As** chooses source, Live Preview or the Reading view for files you open.
- Live Preview stays smooth as you type and scroll in long notes full of tables, math and diagrams.
- Live Preview shows `#tags` as colored pills, as in Obsidian.
- Live Preview raises footnote references like `[^1]` as small superscript numbers, as Reading view does.
- Live Preview hides the backslash of escaped characters like `\*` until the caret reaches them.
- Links in Live Preview are underlined, and wikilinks to notes that don't exist yet look faded.
- In Live Preview, a link's underline fills in under the pointer; `⌘`-click it to open it.
- Callouts like `> [!tip] My title` show their own title, and Obsidian's callout types are recognized.
- Callouts are tinted in their color in both Live Preview and Reading view.
- Each Obsidian callout type, like `[!bug]` or `[!example]`, has its own color and icon.
- Callouts marked `[!faq]-` start folded and `[!faq]+` start open; click the chevron to fold or open them.
- Embedded notes like `![[note]]` or `![[note#Heading]]` show their content in place, in both views.
- Click the name above an embedded note to open it.
- `%%comments%%` look faint in Live Preview and are hidden in Reading view.
- Links to web pages in Live Preview end with a small external-link icon.
- Reading view shows `#tags` as pills, as Live Preview does.
- Links in Reading view are faintly underlined, and wikilinks to missing notes look faded, as in Live Preview.
- A folded callout stays folded when you edit its title.
- Block embeds like `![[note#^id]]` show that paragraph, list item or block in place.
- Block ids like `^id` look faint in Live Preview and are hidden in Reading view.

## 0.20.1

- In **Sessions**, `⌘`-click, `Shift`-click, `Shift`+arrows and `⌘A` select several sessions.
- **Copy Session ID** on one of several selected sessions copies all their IDs, one per line.
- Drag one of several selected sessions to open all their transcripts, or type their paths in a terminal.
- A tab dragged onto another project's square stays under the pointer as that project shows, to drop in a column.
- **View → Attach to Terminal in herdr-core…** shows one of its terminals in a terminal here, attached.
- Closing such a terminal, or `Ctrl+\`, detaches it, and the shell in herdr-core runs on.
- `mayoi run view.attachHC=<address>` shows a terminal of herdr-core by its address, for plugins too.
- On macOS, VoiceOver no longer crashes Mayoi on text that starts with a byte order mark.
- Clicking a Git diff's toolbar buttons while its changes refresh no longer crashes Mayoi.
- On a Mac with Apple silicon, the Intel build of Mayoi updates to the Apple silicon build.
- The Intel build on Apple silicon says at launch that it runs slower and can crash, with a **Download** button.

- Right-click a session in **Sessions** to copy its transcript file, open it in the editor, or reveal it.
- Drag a session from **Sessions** to the editor to open its transcript, or to a terminal to type its path.
- A resumed session's tab keeps the session's title in a shell other than your default one.
- On Linux and Windows, `Page Up` in the **Sessions** list goes back up to the first session.
- Opening a file in Mayoi while it is still starting no longer crashes; the file opens once it's ready.
- On Linux, letters no longer crowd together or leave gaps in fonts with kerning, such as Microsoft YaHei UI.
- **Settings → Editor → Tab Size** sets how many columns a tab takes, 4 by default.
- Tabs now take the same width on every system, where Linux showed them 8 columns wide.

## 0.20.0

- Plugins can add coding agents with `contributes.agents`, whose sessions show in **Sessions** with their own icon.
- A plugin agent's sessions come from files a glob matches, or from a function registered with `agents.register`.
- A plugin agent's session resumes in an editor tab with its `resume` command, as the built-in agents' do.
- A slow or failing plugin agent never holds up **Sessions**, and its error shows in **Plugins → Show Plugin Log**.
- Turning a plugin off fades its agent's sessions out of **Sessions**.
- A resumed session's tab is titled after the session until the agent names it.
- Terminal tabs kept in herdr-core keep their agent's icon across a restart.
- Resuming a session in **Sessions** opens it in a tab of the editor column you were in, ready to type.
- A session already open in a terminal shows that tab again instead of starting another.
- Each **Sessions** row shows an icon for its agent: Claude Code, Codex, Qoder or Grok.
- A terminal tab running a coding agent shows that agent's icon.
- In **Sessions**, `Page Up` and `Page Down` move the chosen session by a page, from the search field or the list.
- `Home` and `End` in the **Sessions** search field choose the first or last session (`Ctrl` on Windows and Linux).
- The chosen session glides to its new row and keeps the next row in view as the keys move it.
- The chosen session is ringed while the **Sessions** list has the keyboard, and dims when the keyboard is elsewhere.
- A session that drops out of **Sessions** fades out faster, and one that comes back mid-fade picks up where it was.
- Clicking a repository's name or row background in source control expands or collapses its changes.
- **Settings > Shortcuts** lists **Report a Problem** under Help instead of Account.
- A `mayoi://file` link opens its file at the line and column it names.
- On Linux, Mayoi is offered for text, Markdown, JSON and other text files, and `xdg-mime default` takes it.
- On Linux, opening a file in Mayoi from the file manager opens that file.
- On Windows, **Open with** lists Mayoi for text, Markdown, JSON and other text files.
- LocatorJS's **Custom link** reaches Mayoi with `mayoi://file/${projectPath}${filePath}:${line}:${column}`.
- A `mayoi://` link to a missing file says Mayoi could not open it, in the window in front.
- Math with `\tag{…}` now renders in the Markdown preview, its label beside the equation.
- After a full-screen program is killed, scrolling or typing in the terminal no longer prints escape codes.
- The sidebar's new **Sessions** view lists Claude Code, Qoder, Codex and Grok sessions, the latest first.
- Sessions show for **This Project** or **All Projects**, and the search field narrows them by title, folder or agent.
- Enter or a double click resumes a session in a new terminal in its folder.
- **Sessions** groups sessions under Today, Yesterday, Previous 7 Days and Older, each with its count.
- Each session row shows its title on one line, with its agent, folder and last use under it.
- While **Sessions** reads for the first time, it shows placeholder rows, which the sessions then replace.
- Claude Code sessions of every `~/.claude-*` folder show too; `agentSessionsClaudeDirs` adds more folders.
- With `terminalBackend` set to `"hc"`, a running session's dot and words say whether it works or waits for you.
- Resuming a session that runs in an hc terminal shows that terminal instead of starting another.
- In **Sessions**, `↑` and `↓` in the search field move the chosen session without leaving the field.
- The chosen session stays chosen when sessions are read again, and sessions that appear fade in.
- Hovering a session shows buttons to resume it or copy its resume command.
- In **Sessions**, the search words are marked in each session's title, agent and folder.
- With the keyboard in the sessions list, the chosen session shows its resume and copy buttons.
- `⌘C` in the sessions list copies the chosen session's resume command.
- The **This Project** / **All Projects** switch in **Sessions** no longer clips in Japanese.
- When a search matches a session's first prompt, folder or id, its row shows that text under the title.
- With `terminalBackend` set to `"hc"`, a working session's dot breathes, and one that starts waiting pulses.
- **Sessions** rows sit slightly apart, and a hovered row's background fades in and out with its buttons.
- Sessions that appear as **Sessions** is read again open their place, and the rows under them slide down.
- Sessions that disappear on a re-read close their place as they fade, and the rows under them slide up.
- Typing in the **Sessions** search field stays quick with thousands of sessions.
- When a session starts or stops running, its state slides in or out and the agent beside it glides over.

## 0.19.3

- Language server plugins can run a `resolve` command that sets the server's arguments, environment and settings.
- Language server plugins can map paths so definitions and diagnostics keep the workspace's symlinked paths.
- Mayoi no longer waits on a language server that has already stopped.
- The command palette, Quick Open and pickers show at once when a key opens them, and fade in on a click.
- Animations share a few lengths, so what moves alike now moves alike.
- Choosing a tab no longer makes its title bolder and shifts the tabs after it.
- The caret's line and column in the status bar, and find's match count, no longer jiggle as digits change.
- Remote SSH and Mermaid commands show their menu in the command palette instead of a bare `:`.
- A terminal whose shell exits as it starts stays open with what it printed, and offers **Report…**.
- **Help → Report a Problem…** opens a new GitHub issue with Mayoi's version and your system filled in.
- Inline blame, commit tips and settings sync show a date for anything older than a week.
- The merge view's count of resolved files reads "1 of 1 file resolved" in English.
- Inline blame now shows with **View → Word Wrap** on, after the caret line's last row.
- When a wrapped line's last row is too full, its inline blame is cut short with `…`.
- Dragging over code in a diff selects text across lines; dragging on the line numbers still selects rows.
- Double-clicking a word in a diff selects it, and a triple click selects the line; `E` opens the file.
- `Cmd+C` in a diff copies the selected code with its indent, without line numbers or `+`/`-` marks.
- Side by side, a selection stays on the side where it started.
- `Cmd+F` in a diff tab finds in the lines it shows on both sides, in every file, with a match count.
- `Enter` and `Shift+Enter` in a diff's Find go to each match, unfolding a collapsed file to show it.
- **Git → Collapse All Files in Diff** and **Expand All Files in Diff** fold the current diff's files.
- On macOS, `Option+Cmd+[` collapses all diff files and `Option+Cmd+]` expands them.
- `mayoi settings` reads and changes preferences, with Mayoi running or not.
- `mayoi settings --folder` in a project's subfolder changes the project's `.mayoi/settings.json`.
- Choosing **Overwrite** for a remote file that changed on its host no longer asks again while an autosave runs.
- After a remote connection is lost, the banner shows its first try to reconnect as attempt 1.
- A question that comes up in the background now waits instead of replacing the dialog you're answering.
- Questions that come up in the background, as a remote file's conflict, wait for an open picker to close.
- Pressing `Cmd+Q` or a window's close button again while the unsaved-changes question shows brings it forward.
- Saving a remote file while another save is under way no longer loses a pending save and close.
- **Revert** or `Escape` on the "changed on host" question keeps the tab open instead of dropping the save.
- Autosave no longer writes to the host while the "changed on host" question waits for an answer.
- With format on save, saving twice in a row waits for the formatter instead of writing unformatted text.
- Chained shortcuts from `runCommands` wait for Git commands in a selected submodule or nested repository.
- During a merge, rebase or cherry-pick, branch switching, stash, fetch, pull and push stay disabled.
- The mascot hops for Git commands in the selected repository, not only the folder's own.
- After switching folders, **Quick Open** lists the new folder's files without waiting for the old folder's listing.
- Files Git ignores show faint right after switching folders, without waiting for the old folder's read.
- Submodules and nested repositories are still found when **Initialize Repository** runs during a scan for them.
- **Running Plugins** and the **Plugins** settings page agree on the state of a plugin that is off or can't load.
- A language server that failed to start tries again when you open a file of its language 30 seconds later.
- Reloading, restarting or turning on a plugin starts again its language servers that failed to start.
- A language server that fails to start shows its error once, when it fails, instead of on every completion.

## 0.19.2

- With `terminalBackend` `"hc"`, terminals show up in the Herdr app, in a space for each folder.
- Terminals already running in herdr-core show up in Herdr too once Mayoi attaches them again.
- Set `terminalBackendHcShowInHerdr` to `false` to keep new terminals out of the Herdr app.
- Overwriting an SSH file that changed on its host no longer swaps the editor back to the host's old text.

## 0.19.1

- **Word Count** and **TODOs** come with Mayoi but start off; turn them on in **Settings → Plugins**.
- A plugin's `plugin.json` can say `"enabled": false` to start off until you turn it on.

## 0.19.0

- **Plugins → Show Running Plugins** opens a live table of each plugin's state, CPU time, memory and crashes.
- Click a column head in **Running Plugins** to sort by it; values refresh every second while the tab shows.
- Each row of **Running Plugins** can restart its plugin, turn it off, open its details, or show its log.
- **Plugins → Show Plugin Log** opens a live log that follows new lines, with a **Jump to latest** pill.
- Filter the **Plugin Log** by plugin, level and text, and use **Copy** and **Clear** in its bar.
- The **Plugin Log** keeps the last 10,000 lines and stays fast however long it gets.
- A plugin view's open folders and selection are now kept per window.
- Two lists in the same plugin view no longer show the same row as selected.
- Built-in status bar items that do nothing on click, such as the problem at the cursor, show their tooltip again.
- A plugin's settings now live only in its details; its row on **Settings → Plugins** links to them.
- Plugin sidebar views, such as **TODOs**, match the Explorer's rows, file icons, hover and selection.
- Plugin sidebar views show a loading state, a clear empty state, and fade in when their content arrives.
- Plugin status bar items, such as the word count, glide to a new width instead of shifting their neighbours.
- Plugin status bar items sink when pressed and show their tooltip even when they have no command.
- Plugin pick boxes look like the command palette, with its key hints and a shake when nothing matches.
- The command palette, pickers and prompts fade and scale in as they open, unless Reduce Motion is on.
- Click a plugin on the **Settings → Plugins** page to open its details; `Esc` goes back to the list.
- A plugin's details list its commands with their shortcuts, menus, status items, views, themes and more.
- A plugin's details show its memory, CPU time and start time, refreshed every second while open.
- A plugin's details show its recent log lines and its settings, with **Open Full Log** for the whole log.
- Plugin errors now show on the Plugins page as soon as they happen.
- The disabled **Install** button on the Plugins page now looks clearly disabled in dark themes.
- `mayoi plugin new` fills in `author` from your Git user name.
- **Find in Files** (`Shift+Cmd+F`) selects the previous query, so typing or pasting replaces it.
- Drag a file's tab onto a project in the rail to move it there; hold `Option` to copy it instead.
- Rest a dragged tab on a project in the rail to switch to it, then drop the tab where you want it.
- Unsaved changes come along when a tab moves to another project.
- Right-click a tab and choose **Move to Project** to send it to another project.
- New plugins: folders of JavaScript or TypeScript in `~/.mayoi/plugins`, which Mayoi runs with no Node or build step.
- Plugins add commands, shortcuts, menu items, status bar items, sidebar views, previews, themes, languages and snippets.
- `mayoi plugin install <git-url>` installs a plugin, and `list`, `update` and `remove` manage them.
- `mayoi plugin new <name>` writes a new plugin, and `mayoi plugin dev` reloads it each time you save.
- `mayoi plugin pack` copies the npm packages a plugin imports into it.
- The **Plugins** menu, and the **Plugins** page of the settings window, install, update, remove and configure plugins.
- **Plugins → Show Plugin Log** and **Show Running Plugins** show what plugins logged and how much they use.
- A plugin that loops or keeps too much memory is stopped, and `mayoi --disable-plugins` turns plugins off.
- The **Plugins** settings page shows each plugin's version, author, source, and a colored dot for its state.
- The **Plugins** page asks before it removes a plugin, and shows install and update errors on the plugin's row.
- While the plugin index loads, the **Plugins** page shows placeholder rows, and **Try Again** if it fails.
- Switches in the settings window respond to hover and stretch while held.
- A plugin's `plugin.json` and its index entry can name an `author`.
- The **Plugins** page shows no error while the plugin index isn't published yet.
- **Selection → Format Document** (⇧⌥F) formats with a plugin, and the `formatOnSave` setting formats as you save.
- Plugins underline problems in the editor; the status bar counts them, and **Show Problems** lists them.
- Plugins can bring language servers, for **Go to Definition** (F12), **Show Hover** and **Complete Word** (⌃Space).
- Plugins can add themes, including VS Code color themes as they are.
- **Selection → Insert Snippet…** inserts snippets that plugins bring, in VS Code's format.
- Each function a `.ts` file in `~/.mayoi/scripts` exports is a command; **Run Selection as Script** runs code once.
- Signed in, the plugins you installed from Git follow you to your other machines.
- New **Word Count** plugin shows the words of a Markdown file and its reading time in the status bar.
- New **TODOs** plugin lists the folder's TODO, FIXME and HACK comments in the sidebar.

## 0.18.2

- On Windows, right-click menus no longer cut off the end of their labels, as in Chinese.
- The Markdown preview highlights `==text==` and `<mark>` text with a yellow background.
- In Markdown, typing `=`, `*`, `_`, `~` or `$` over a selection wraps it; type it twice for `==` or `**`.
- The Markdown editor highlights `==text==` with a yellow background, as Obsidian does.
- The Markdown editor colors bold, italic and struck text between Chinese characters, as in `会**整体**线`.
- The Markdown editor shows `**bold**` in bold and draws a line through `~~struck~~` text.

## 0.18.1

- Opening a folder you switched away from no longer shows the terminals another window kept from it.
- On Windows, lines starting with tabs, like Go code, keep their indent, at four columns a tab.
- On macOS, Mayoi appears in Finder's **Open With** menu for text, Markdown and source code files.
- AI commit messages leave out the lines of more secret files, like `.envrc`, `*.tfvars` and `~/.aws` files.
- A changed `.env` or key file that is already committed no longer has its lines sent to the AI either.
- Keys, tokens and passwords in any changed line are swapped for `[left out]` before the diff goes to the AI.
- New `aiSecretFiles` setting adds your own patterns of files whose lines AI commit messages leave out.

## 0.18.0

- AI commit messages no longer send the contents of new secret files like `.env` or keys, only their names.
- With the `"hc"` backend, `terminalBackendHcPath` finds hc before `PATH` and `~/.local/bin`.
- With the `"hc"` backend, `terminalBackendHcEnv` passes `KEY=value` variables to hc's commands.
- With `terminalBackendHcPath` set, Mayoi no longer offers to install hc.
- `terminalBackendHcPath` and `terminalBackendHcEnv` stay on each machine and never upload with settings sync.
- Rename a terminal from its tab's right-click menu or **View: Rename Terminal…** in the command palette.
- A renamed terminal keeps its name when programs set a title; rename it to nothing to show theirs again.
- Pressing Enter in the crash report question chooses **Don't Send** instead of sending the report.
- On Windows, files indented with tabs, like Go code, show their indentation again.
- Find's match count changes instantly instead of rolling, in the editor, preview and terminal.
- The Git commit list and project rail use less CPU redrawing author and project initials.
- Windows can install `mayoi.cmd` from **File → Install 'mayoi' Command**, including user PATH setup.
- The host box of **Connect to Host…** has an **SSH Config** button that opens `~/.ssh/config` in a tab.
- **File → Remote SSH → Open SSH Config** and the command palette open it too, creating it when missing.
- Typing in Markdown with blank lines between paragraphs takes less CPU to keep the colors up to date.
- Terminals in tabs you can't see no longer redraw the window as they print, which saves CPU.
- Typing an absolute or `~/` path in Quick Open offers that file, even with no folder open.
- A path in Quick Open can end in `:line` or `:line:column`, and opens the file at that spot.
- Quoted or backslash-escaped paths pasted from a terminal open in Quick Open too.
- A project whose program works shows a turning ring at the corner of its square on the rail.
- When work finishes or fails while other work goes on, its dot shows inside the ring.
- The rail marks a project amber while it waits for you, green when done and red when work failed.
- A program waiting for you keeps its project's mark until it stops waiting, even after you look.
- Hovering a project on the rail lists what its programs wait for and work at, with their progress.
- The work ring on a rail square turns smoothly and fills to show progress when programs report it.
- A rail square's ring and dot grow in and fade out, and the dot blends from one state's color to the next.
- The rail tip marks waiting, finished and failed work with icons and shows progress as a small bar.
- A rail square's progress ring keeps its fill as it fades out, and completes when the work finishes.
- A rail square's dot sends out one ring in its color when work finishes or fails.

## 0.17.0

- Terminal icons show whether a program says it works, waits for you, finished or failed (OSC 7501).
- While a program works or waits for you, a pill over its terminal says what it does, with its progress.
- A program in a project out of sight that waits for you, finishes or fails marks the project on the rail.
- Typing in a terminal clears the mark of a program's finished or failed work.
- The program status pill shows the program's name in bold, its message, and its progress as a bar.
- When a program waits for you, its pill gets a colored ring and its icon pulses in time with the badge.
- As shown work finishes or fails, the pill says so with a check or a cross for a moment.
- The pill counts the program's other active tasks, and the terminal icon's tooltip lists them.
- A program that reports its status very often no longer slows the window down.
- **Rename Active File…** in the command palette renames the current tab's file, bindable as `file.rename`.
- Opening a file from outside the folder clears the selection in the Explorer.
- Splitting a Markdown preview to the right keeps it a preview, scrolled to the same place.
- **New File…** and the other items of the Explorer's menu below the files no longer do nothing at times.
- A plain mouse wheel scrolls the editor tabs sideways when more of them are open than fit.

## 0.16.2

- **View → Theme → The Matrix** puts the code's green inside steel chrome, like the film's real world.
- The Matrix gives each kind of code its own hue, and shows comments in steel that reads at 6.9:1.
- Under The Matrix, primary buttons are the red pill, chosen rows the blue, and Discard is orange.
- The White Rabbit is The Matrix's character on the welcome page and in the status bar, hopping while Git runs.
- The Matrix's digital rain is mirrored, as in the film, with a dimmer far layer behind it.
- Quick open, the command palette, pickers and dialogs now show at once instead of fading in.
- The find box, the sidebar and the panel appear instantly when opened with their shortcuts.
- Switching tabs or sidebar views no longer slides or fades the marks and the view.
- The selected row in quick open and pickers moves at once with the arrow keys.
- Vim's mode badge changes color at once on `i` and `Esc`.
- The theme picker shows all its rows at once, and moving through it switches the theme without a fade.
- Settings pages now fade in quickly without rising, and pressed buttons sink a little less.
- Mermaid diagrams copy as an image from their hover buttons or the opened diagram's header.
- Mermaid diagrams export as PNG or SVG from their export button, in the colors of the current theme.
- `.mmd` previews copy and export their diagram with **Copy Mermaid Diagram as Image** and the export commands.
- Pinching a touchpad zooms images, SVG and Mermaid previews, and opened diagrams around the pointer.
- On Windows, Ctrl with two-finger scrolling now zooms a picture instead of doing nothing.
- **View → Theme → Claude** now looks like Claude Code in a terminal, with the whole window in the code's font.
- Under Claude, the status bar shows Claude Code's ✻ spinner, the work's name and its seconds while Git runs.
- Claude's errors and removed lines are rose now, apart from its orange, and chosen rows are periwinkle.
- In Claude Light, the terminal's white and bright white text is readable on the light background.
- **View → Compact Folders** shows chains of single-child folders on one Explorer row.
- The Explorer header's context menu, the settings window and `explorerCompactFolders` toggle it too.
- Hover a compact row to see its whole path when the sidebar cuts it off.
- `⌘F` in a terminal opens a find bar that searches its output and scrollback, highlighting each match.
- In the terminal's find bar, `Return` goes to the match above and `Shift+Return` to the one below.
- On Linux and Windows, `Ctrl+F` in a terminal opens find instead of reaching the shell.
- Under Purple Gold, the welcome page drops the mosaic apse and arch and matches the other themes.
- In the find bar, `Shift+Return` now goes to the previous match instead of the next one.
- In the find bar's replace field, `Shift+Return` replaces the match and goes back to the one before it.
- In **Search**, `Shift+Return` in the query selects and previews the previous result, wrapping to the last.
- The Qoder theme lays the editors and terminal on a rounded card, in a grained shell without dividing lines.
- In the Qoder theme, dragging a sidebar or panel edge lights the edge only around the pointer.
- The Qoder theme titles the sidebar, Source Control and the terminal in sentence case, as Explorer.
- The Qoder theme colors code in Qoder's own chart colors: green, blue, violet, amber and rose.
- In the Qoder theme, Qoduck holds up a magnifier while searching and a laptop while Git works.
- Qoduck wears a headset while AI writes a commit message, and raises a megaphone when a toast appears.

## 0.16.1

- **View → Theme → Purple Gold** is now Porphyry: Tyrian purple chrome, gold leaf and gold hairlines.
- Under Purple Gold, a gold mosaic apse shimmers behind the welcome page, under an arched card.
- Purple Gold has a character, Corona, a jeweled crown that blinks, glances and hops while Git works.
- Purple Gold's code is in amethyst, gold, rose quartz and jade, and the open tab gets a gilded line.

## 0.16.0

- Turn on `editorAgentContext` to show a gutter **+** in ordinary files that collects code with a comment.
- Leave the comment empty and choose **Add to Agent** to collect the code alone.
- Code collected across files goes to the last terminal together with **Send N Items**.
- The **…** button above the editor lists collected items to go to, edit or remove; they survive restarts.
- **Mayoi → Settings…** (⌘,) opens a settings window with every preference by category, and a search.
- Changes in the settings window apply to every window at once, and each one resets with one click.
- **File → Keyboard Shortcuts…** lists every command's keys; double-click one and press its new keys to change them.
- The shortcuts editor warns when other commands use the keys, and keeps the comments of `keybindings.json`.
- A folder can set preferences such as `vim` or `autoSave` for itself in `.mayoi/settings.json`.
- Pick a folder at the top of the settings window to change its own settings rather than yours.
- `settings.json` and `keybindings.json` still open as files from **File → Open Settings (JSON)** and the like.
- **View → Theme** adds three Halloween themes, always dark: Jack-o'-Lantern, Witch's Brew and Blood Moon.
- Jack-o'-Lantern is pumpkin orange by candlelight, with a carved pumpkin on the welcome page and status bar.
- Witch's Brew is plum dark with slime green, and a bubbling cauldron for its character.
- Blood Moon is crimson and old gold on a red-tinged black, with a bat that flaps its wings as it hops.
- The settings window groups each section's settings in cards, and marks the ones you changed.
- The settings sidebar shows how many settings you have changed in each section.
- **Keyboard Shortcuts** shows each key as its own cap, and tags your own keys and removed ones.
- The dialog for new keys shows them large, and turns amber when another command has them.
- **Settings → Appearance** shows every theme as a small preview window; click one to switch to it.
- Font size, weight, line height and window zoom have sliders that show their value as you drag.
- **Settings → Fonts** shows sample code in your font, and it updates as you change the font settings.
- Settings with two or three short choices, such as `appearance`, are a row of buttons instead of a menu.
- Resetting a setting or a command's keys shows a notice with **Undo** for a few seconds.
- **Keyboard Shortcuts** groups commands by menu, and each menu's name stays at the top as you scroll.
- Filter the shortcuts to **All**, **Changed by Me** or **Conflicts**, each with how many it shows.
- Keys that two commands share show in amber with a warning sign in the list itself.
- In the shortcuts list, arrows choose a command, `Enter` changes its keys and `Delete` removes them.
- `⌘F` in the settings window goes to its search, and `↓` goes on from the search to the list.
- The arrow keys switch settings sections, and `→` or `Enter` moves into the section shown.
- **Settings → Fonts** picks the font from your installed monospaced fonts, each shown in itself.
- The font sample previews the font you point at or arrow to in the list, before you choose it.
- Text settings, such as the font, apply when you press `Enter` or leave the field, not at each key.
- A folder's own setting changed in the settings window no longer, now and then, snaps back.
- Headings in the Markdown preview take the color theme's heading color, as the editor does.
- Command-clicking a terminal path or URL followed by Chinese punctuation no longer takes the text after it.
- Switching back to a tab no longer flashes a blank editor for a frame before its text shows.
- When a file changes on disk, the editor stays where you were reading instead of jumping to the caret.
- The caret stays on its line when a file changes on disk above it, rather than drifting to other text.
- Select All no longer flashes the end of the file before the view settles.
- With word wrap on, switching back to a tab returns to the same lines, not lines nearby.
- Switching back to a tab whose caret is out of view no longer flashes a blank editor.
- With word wrap on, Enter, Tab, undo and redo no longer move the text around the screen.
- Pressing Enter or moving a line at the edge of the editor scrolls one line, not a third of the page.
- Clicking or holding near the right edge of a long line no longer runs the text sideways.
- Find scrolls a match at the top of the editor out from under the find bar.
- Deleting the longest line shrinks how far the editor scrolls sideways.
- A tab you switch back to shows the same lines after edits in another column added lines above them.
- With word wrap on, moving a tab to another column keeps the same lines in view.
- The folder button by the branch in source control opens **Switch Worktree…** to change checkouts.
- **Switch Worktree…** finds a checkout by folder or branch; Enter on a new name makes a worktree.
- Typing an existing branch's name in **Switch Worktree…** points to **Switch Branch…** instead.
- **Switch Branch…** lists local branches first, with each branch's last author and time.

## 0.15.0

- The Homebrew install steps add `brew trust sorrycc/mayoi`, which recent Homebrew needs to load the cask.
- Set `terminalBackend` to `"hc"` to run terminals in herdr-core, where shells outlive Mayoi's windows.
- Closing a terminal ends its shell in herdr-core, and `Ctrl+\` goes to the shell rather than detaching.
- Terminals in herdr-core attach again when Mayoi starts, with their scrollback, in their tabs and the panel.
- In a remote folder, terminals in herdr-core run on the host as one of hc's machines.
- Mayoi asks once before adding a host to hc, and its terminals use ssh until then.
- **View → Install hc…** installs or updates `hc` in `~/.local/bin` with herdr's installer, on macOS and Linux.
- With `terminalBackend` `"hc"` and no `hc` installed, the first new terminal offers to install it.
- Mayoi finds `hc` in `~/.local/bin` even when your shell's PATH lacks it, so it works right after installing.
- New terminals in herdr-core open at once and say they are starting, rather than freezing the window.
- A dot on a terminal's tab and panel chip shows that its shell runs in herdr-core; hover it for its address.
- The dot pulses, and a pill over the terminal says what it waits for, while it starts or its machine is away.
- The dot is cut out of the terminal icon by a thin ring, and its color fades as the terminal's state changes.
- A failed add to hc says why, suggesting a login only when hc asks for a password.
- When hc can't run on a host, Mayoi suggests installing it there and asks again on the next terminal.
- Once a terminal reaches herdr-core, its dot flashes and the pill shows a check and **Running in herdr-core**.
- A terminal fades while it waits for its machine, and comes back to full brightness once reattached.
- Hover a terminal's dot while it starts or waits to see what it waits for.
- The status bar shows a spinner while hc installs or a host is added to hc.
- A terminal stays faded while its shell starts in herdr-core, and brightens once it attaches.
- A terminal in herdr-core whose machine is away says its shell runs on there, rather than that it restarts.
- A waiting terminal's dot and spinner settle after a few seconds, so a long wait doesn't keep redrawing.
- The status bar shows a spinner while hc installs or a host is added to hc.
- A terminal stays faded while its shell starts in herdr-core, and brightens once it attaches.
- Mayoi starts faster with terminals of other machines to attach, and waits for a machine that is away.
- What you type in a terminal while it starts in herdr-core reaches the shell once it attaches.
- **Copy hc Address** in a terminal's menu copies its address in herdr-core, for `hc attach` elsewhere.
- The panel's terminals in herdr-core attach again when Mayoi starts, rather than starting new shells.

## 0.14.3

- Mermaid diagrams in a Markdown preview open over the window from their hover button or a double-click.
- An opened diagram zooms with its `−`/`+` buttons or `Cmd`-scroll, and drags to move; `Esc` closes it.
- Bold and italic in a Markdown preview render when CJK punctuation sits next to Latin text.
- Mayoi no longer crashes drawing a terminal after running for hours.
- Crash reports include what Mayoi printed before it crashed, so aborts from macOS can be tracked down.

## 0.14.2

- The color theme box opens scrolled to the theme you're using.
- `Cmd+A` in the editor selects all without scrolling to the end of the file.
- A branch with a GitHub pull request shows `#123`, its checks and review in Source Control and the status bar.
- Click the pull request badge to see its changes in a tab; right-click it for **Open on GitHub**.
- **Git → Open Pull Request…** lists open pull requests, or opens any one by its number; it needs `gh` signed in.
- A pull request tab shows the same changes as GitHub, inline or side by side, with its state and branches on top.
- Check **Viewed** on a file of a pull request to fold it and mark it viewed on GitHub, as its own checkbox does.
- When new commits reach the pull request, its tab offers **Reload** to show them.
- Commits that head an open pull request show a `#123` chip in the history; click it to open its changes.
- **Git → Show Pull Request Checks…** lists the CI checks of the branch's pull request and how each went.
- Click the badge's checks icon, or right-click the badge for **Show Checks…**, to open the same list.
- **Send to Terminal** on a failed job pastes the end of its log into the last terminal, without pressing Enter.
- **Re-run** runs a failed job again; **Copy Log** and **Open on GitHub** are there too.
- **Git → Open File on Remote** opens the file on GitHub, GitLab or Bitbucket at the caret line or selected lines.
- **Git → Copy Permalink** copies a link to the selected lines at the current commit.
- Cmd-click or Shift-click commits in **Source Control** to select several; Shift+arrow keys extend the range.
- Press `Cmd+C` in the commits list to copy the short hashes of the selected commits, one per line.
- Both are also in the tab and Explorer file context menus.
- **Stash Changes…** and **Stash Including Untracked…** in the Source Control **⋯** menu stash your changes.
- The **⋯** menu's **Stashes** submenu applies, pops or drops one of the last ten stashes; dropping asks first.
- **Git → Stashes…** lists every stash to search, with ⏎ to apply, ⌘⏎ to pop and ⌘⌫ to drop.
- Tall Mermaid diagrams in a preview show their boxes and arrows, not just their labels.

## 0.14.1

- **View → Theme → Purple Gold** is always dark: a near-black sidebar and a royal purple status bar.
- The Purple Gold theme uses classic gold for buttons, chosen rows and highlights, and golds and lavenders for code.
- The Cursor theme's current line, word highlights, find matches and hovered rows now match Cursor's own.
- Commit tooltips in the history no longer pop up over the rows while you scroll.
- Right-click a folder in **Explorer** and choose **Find in Folder…** to search only that folder.
- Bold, italic and strikethrough now render in the Markdown preview when quotes or brackets touch Chinese text.
- Vue files keep their colors after a directive value that prettier wraps over several lines.

## 0.14.0

- **View → Theme → Cursor** takes Cursor's own Cursor Light and Cursor Dark, as the Appearance is.
- The Cursor theme colors the code and the terminal as Cursor does, with its blue for buttons.
- Press `⌘⏎` in quick open to open the chosen file in the column to the side, splitting one if needed.
- The Markdown preview shows `[[note]]` and `[[note|label]]` wiki links, and a click opens the note.
- Wiki links find their note next to the file, at the repository's root, or anywhere in it, as Obsidian does.
- `[[note#heading]]` opens the note at that heading, as do Markdown links to `other.md#heading`.
- `![[image.png]]` shows the image in the Markdown preview, and `![[image.png|300]]` sets its width.
- `⌘`-click a `[[wiki link]]` in a Markdown file's text to open its note.
- The find bar's match count shows in full in every language, even with thousands of matches.
- In Chinese, the find bar's match count reads `1/5`, as in Japanese.

## 0.13.1

- When an update check fails, the update window shows the whole error, with no tooltip over it.
- The panes of a merge wrap long lines when **Word Wrap** is on, and `⌥Z` toggles wrapping for that merge tab.
- Conflict marks, gutter buttons and synced scrolling in a merge follow wrapped rows.
- A conflict taller than its pane keeps its gutter buttons in view while any of it shows.
- A sideways swipe or `Shift`+wheel scrolls the result pane of a merge, as it does the side panes.
- `⌘Z`, `⇧⌘Z`, `⌘X`, `Tab` and `Enter` show their edit at once, without the line highlight flickering.
- Undo and other edits no longer recenter the view when the caret is near its bottom edge.
- With **Word Wrap** on, undo and redo keep the editor where it is scrolled instead of jumping.

## 0.13.0

- **Close Other Projects** asks before closing the window's other projects.
- `⌘F` in a Markdown preview finds text in the rendered page, without switching to the source.
- Matches in a Markdown preview are highlighted, and `⌘G` and `⇧⌘G` scroll to each one.
- Typing and scrolling are smoother in files of Chinese, Japanese or other non-ASCII text.
- Window zoom scales the whole interface, including sidebars and terminals.
- Use `Cmd` on macOS or `Ctrl` on Windows/Linux with `=`, `-` or `0` to zoom or reset.
- Set `zoomLevel` in `settings.json` to save a window zoom level, including decimals.
- The command palette has separate commands for editor font size and image zoom.
- `⌘=` / `⌘-` / `⌘0` now zoom the whole window; editor font size and image zoom moved to the command palette.
- `⌘=` and `⌘-` no longer grow the editor font or zoom an image with focus; bind `editor.fontZoomIn` or `image.zoomIn` in `keybindings.json` for the old behaviour.
- Going to a line, a symbol or a search result briefly lights the line the caret lands on.
- The sidebar and the terminal panel fade in as they open.
- A folder's files fade in as it expands in the Explorer.
- The breadcrumbs' folder box and the account panel ease in, and project tips fade in.
- Typing in long files and scrolling diffs do less work each frame.
- Rows fade in as a group, a folder or a commit opens in **Source Control**, and as files change.
- A file's matches fade in as it unfolds in the search results.
- Scrolling a file of over 6,000 lines colors the lines ahead before they come into view.
- Typing recolors the edited lines with less work, and windows with many tabs draw with less.
- File tabs gain a **Copy Relative Path** option in their context menu.
- **View → Preview Markdown on Open** opens Markdown and `.mdx` in preview mode.
- `⇧⌘V` switches back to the source; new files, search results and line jumps always open as source.
- Restored and reopened Markdown tabs keep their view and scroll position, including offline remote tabs.
- MDX previews omit top-level `import`/`export` blocks, preserving code examples and paragraph text.
- Files dragged from **Explorer**, **Source Control** or **Search**, and folders from **Explorer**, drop into other apps.
- A remote file or folder dragged to another app arrives as its `host:/path` text.
- **File > Install 'mayoi' Command** uses a folder on your shell's `PATH`, or says how to add one.
- Uninstalling removes `mayoi` from every folder it was installed in.
- Install and upgrade on macOS with Homebrew using the `sorrycc/mayoi` tap.
- Homebrew installs the `mayoi` shell command and removes it when uninstalling the app.
- The Homebrew `mayoi` command reports when Mayoi.app cannot be found.

## 0.12.3

- MyGo 0.7.1: Shift-click extends terminal selections, and Powerline's rounded caps fill the line.
- Mermaid nodes and subgraphs with a light `style` fill keep their labels readable in dark themes.

## 0.12.2

- **Source Control** shows a folder's repositories and submodules in one list that scrolls as a whole.
- Repository and change-group headers in **Source Control** stay pinned at the top while their files scroll.
- Each repository header in **Source Control** has its own branch, remote and **⋯** actions.
- A repository's arrow expands or collapses it; clicking its name shows its history.
- Drag the divider between changes and history in **Source Control** to resize them for the folder.
- Submodule rows in **Source Control** say whether the commit reference or the content changed.
- Command-clicking a file path in a local terminal opens it in the editor, even if Git ignores it.
- Terminal paths ending in `:line` or `:line:column` open with the caret at that spot.
- **Open Preview to the Side** opens the preview in the column beside the file instead of adding one.
- The Markdown preview shows footnotes like `[^1]` as raised numbers, with the notes listed at the end.
- Clicking a footnote's number scrolls to its note, and the note's `↩` scrolls back.

## 0.12.1

- A file with a line too long to show, such as a minified `.map`, no longer opens and freezes Mayoi.
- Such a file left open is not reopened at launch, so Mayoi starts again.
- The Markdown preview draws LaTeX math between `$` and `$` as formulas within the text.
- Math between `$$` and `$$`, or in a `math` code block, shows as a centered formula of its own.
- A formula Mayoi cannot draw shows its source in red, and a block of math also says why.
- `previewMath` set to `false` shows math as its source.
- The Markdown preview links bare URLs right after punctuation such as `：`, as GitHub does.

## 0.12.0

- The Markdown preview draws `mermaid` code blocks as diagrams, in the colors of the theme.
- Hover a diagram for **Show Source** and **Copy Code**; one that cannot be drawn says why.
- `.mmd` and `.mermaid` files preview as their diagram with ⇧⌘V, and zoom like an SVG.
- `previewMermaid` set to `false` shows Mermaid blocks as code.

## 0.11.12

- `mayoi run <id>[=<arg>]` runs any command of the window, and `mayoi commands` lists them.
- `mayoi -g file:line` opens a file at a line, `-d` compares two files, and `-n` opens a new window.
- `mayoi --remote host:/path` opens a folder on an SSH host.
- `mayoi -w` waits for its files to close, so it works as `git config core.editor "mayoi -w"`.
- `mayoi` says what went wrong and exits non-zero when it fails.
- Mayoi updates a `mayoi` command installed by an older version as it starts.
- Opened from the Finder or Dock, Mayoi uses your shell's `PATH`, so git hooks find `pnpm` and `node`.
- Fix a crash on **Go → Go to Symbol…** (⇧⌘O) in a file with a line that starts with `func() {`.

## 0.11.11

- The welcome page has a **Star on GitHub** link.
- Fix a crash on clicking a tab of another column while the find box is open.
- Fix a crash on dropping a tab or terminal from one window into another.
- Crash logs from before 0.11.6 are reported as of an unknown version.

## 0.11.10

- The diff toolbar's menus have their own icons, and the split toggle is gone.
- A commit's file panel is hidden until shown with **Git → Show Files of Commits**.
- **⌘F** and **⌥⌘F** select the find box's query, so typing replaces it.

## 0.11.9

- The Grok Bot theme, with Grok Bot on the welcome page and in the status bar.
- **View → Theme → Color Theme…** previews each theme as you move through the list.
- Changing the theme or appearance fades the colors, unless reduced motion is on.
- A commit's tab lists its changed files in a tree beside the diff.
- The arrow by a commit in **COMMITS** lists the files it changed.

## 0.11.8

- **View → New Terminals in Editor Area** opens new terminals as editor tabs.
- The Markdown preview renders HTML as GitHub does, including centered logos and sized images.
- The Markdown preview shows images from the web, such as badges.
- The vim mode badge switches to the new mode at once instead of fading.

## 0.11.7

- Untitled documents are kept on quit and reopen at the next launch.
- Branch switching, unstaging and discarding work with Git older than 2.23.
- Long paths in diff headers shorten to fit narrow columns.
- Files copied in the Finder paste into a terminal as quoted full paths.
- On macOS, holding a key repeats it instead of showing accents.
- Linux: tab stops and crisp text on Ubuntu 22.04 work again.

## 0.11.6

- After a crash, Mayoi offers to send an anonymous crash report.
- Terminal tabs stay clickable when the tab strip overflows.
- The merge view's bars move actions into a **⋯** menu as the window narrows.
- The status bar's branch opens the same branch picker as Source Control.
- A merge's three panes scroll together.
- The merge gutter's bands stay inside the gutter.
- Command-scrolling over a terminal no longer changes its font size.
- Linux and Windows: a title bar menu button replaces the menu bar.
- Light or Dark appearance holds when the desktop uses the other.
- Maximizing the bottom terminal panel fills the window again.
- MyGo 0.6.0: faster redraws, smooth wheel scrolling, Kitty graphics in the terminal.

## 0.11.5

- Discarding a diff block edits the file's document, as one undo step.
- Unstaged diff blocks have an arrow that reverts the block.
- Control-click the arrow to append the other version's lines instead.
- Edit the working tree side of a diff in place with **I** or **Edit**.
- Diff tabs have **Previous File** and **Next File** buttons and a **Side by Side** toggle.
- Change navigation stops at a file's ends before moving to the next file; see `diffCrossFiles`.
- Opening a file from a diff lands on the current change's line.
- Deleted files offer **Restore File**, and untracked files **Remove File…**.
- The **Whitespace** menu filters whitespace changes in diffs.
- **Highlight** and **Context** menus set word or character highlighting and context lines.
- **Synchronized Scrolling** in the Layout menu lets diff sides scroll apart.
- **Line Alignment** in the Layout menu, or `diffAlignedRows`, turns off row alignment.
- Diffs keep their place when folding, wrapping or resizing.
- **Git → Compare Two Files…** diffs any two local files.
- Conflicted files open in a three-way merge view.
- The merge view lists the files in conflict and applies non-conflicting changes automatically.
- Side-by-side diffs have a gutter of bands linking changes, as in JetBrains IDEs.
- Changed words are compared against the paired line, not the same line number.
- With **Line Alignment** off, both sides of a scrolled split diff stay visible.
- Change navigation glides, and change buttons fade with the pointer.
- The merge view shows full lines, scrolls its panes together, and has gutters with accept buttons.
- A Git sync button in the status bar shows incoming and outgoing commits.
- Linux: fix a crash when the menus change (#51).

## 0.11.4

- Mayoi checks for updates hourly and shows them in the status bar instead of opening a window.
- Mayoi signs in to Mon as the Mac's name and no longer piles up sign-ins.

## 0.11.3

- The Markdown preview's text can be selected and copied.
- A redesigned Markdown preview, closer to GitHub's.
- The Markdown preview shows GitHub alerts such as `[!NOTE]`.
- Links to headings, as `[Usage](#usage)`, scroll the preview.
- **Open Preview to the Side** shows a Markdown or SVG preview in the next column.
- An empty Markdown document's preview says there's nothing to preview.
- Diffs highlight changed words correctly below added or removed lines.
- The Markdown preview parses and draws faster.
- Fitted images and SVG previews cast a soft shadow.
- The Markdown preview scrolls with its text in another column, and the other way round.
- Switching between a Markdown file's text and preview keeps the place.
- Markdown tables are as wide as their text, with round corners.
- The Markdown preview's status bar shows the word count and reading time.
- Images the preview can't show appear as cards with their alt text.
- Heading spacing in the Markdown preview matches GitHub's.
- Images zoomed past their size show crisp pixels.

## 0.11.2

- New Terminal in Editor Area skips locked columns.

## 0.11.1

- Source Control lists the Git repositories inside a non-repository folder (#17).
- Windows: submodules are named with `/`.
- Fix dragging a project's square along the rail.

## 0.11.0

- Settings sync no longer undoes a change made while it syncs.
- Windows: many path fixes for quick open, rename, the Markdown preview and Source Control.
- Windows: terminals are named after their shell.
- `~` shortens only the home folder itself, not folders whose names start the same.
- The Windows fixes follow @wowyuarm's port (#40); tests now run on macOS and Windows.
- The Markdown preview's column is 120 characters wide instead of 80.
- Vim mode records and plays macros: `qa`, `@a`, `@@`.
- Vim mode has registers, `"a`–`"z` and the special ones; `:reg` shows them.
- Vim mode has marks and a jump list; `:marks` lists them.
- Vim mode adds `:normal`, `:g`, `:v`, `:m`, `:t`, `:pu`, `&` and `g&`.
- Vim mode adds `it`, `at`, `[(`, `[{`, `])`, `]}`, `gp` and `gP`.
- Vim mode repeats inserts with a count, as `3ix`.
- Vim mode: ⌃W and ⌃U delete in insert mode on macOS.
- Vim mode: `ma` sets a mark.
- Vim mode's search highlighting does less work per frame.
- Vim mode shows a half-block cursor while an operator waits, and flashes yanked text.
- Vim mode has `/` and `?` search with live highlighting and match counts.
- Vim mode's `:` runs commands such as `:w`, `:q`, `:s`, `:sort` and `:vs`.
- Vim mode adds `gv`, `gi`, `gJ`, `H M L`, `zz zt zb`, ⌃A, ⌃X, `ZZ` and `ZQ`.
- Vim mode matches vim in more edge cases, such as `2D`, `d}` and `di(`.
- Vim mode adds `ip`, `ap`, `3V`, `3v`, and `.` after visual changes.
- Vim mode: `p` over a selection yanks what it replaces.
- Esc after `o`, `O`, `S` or `cc` with nothing typed removes the indent.
- Vim mode's keys work on non-Latin keyboard layouts.
- Find, quick open and the command palette shake when Return finds nothing.
- The find box, change peek and review comment box slide up as they close.
- Find and search fields fade their focus border.
- Lists, buttons and the sync panel fade their hover colors and press down.
- Hover-only buttons fade in and out.
- Splitter lines show after the pointer rests on them.
- Empty-state badges pop in, and a commit that leaves nothing ends with a check.
- Switching sidebar views fades in.
- Source Control's group counts roll to their new numbers.
- Tabs and terminal chips animate as they open, close and move.
- Buttons, tabs, breadcrumbs, the status bar and the welcome page fade their hovers.
- The sidebar's view marker and the picker's selection slide.
- Chevrons rotate as sections open and close.
- Change counts and find's match count roll to their new numbers.
- The vim badge, branch name and title bar cross-fade as they change.
- The welcome page animates in.
- Returning to a wrapped file's tab keeps its scroll position.
- The title bar's box no longer leaves a gap after a short name.
- The welcome page's rows are all one height.
- The vim mode badge in the status bar is colored by mode.
- The command palette shows a ⌘ icon.
- The welcome page lists six recent folders, then **More…**.
- Source Control's message box grows with the message, up to eight lines.
- Cut with no selection cuts the whole line.
- Fix splitting a diff to the right closing the window (#20, #39).
- Quick open and the command palette rank consecutive letter matches higher.
- The Markdown preview underlines large headings and mutes quotes, as GitHub does.
- Quick Open has its own icon on the welcome page.
- Click a folder in the breadcrumbs to browse its files (#38).
- Light themes meet WCAG AA contrast for Git status colors and errors.
- A missing `fontFamily` or `terminalFontFamily` falls back to the system monospace font.
- Breadcrumbs of files outside the folder show `~` and no empty segment.
- Typing in long files re-highlights less.
- Source Control lists each submodule's changes.
- The ✨ button writes commit messages with AI when signed in to Mon.
- **Sign In to Sync Settings…** is now **Mayoi → Sign In…**.

## 0.10.0

- Settings sync: sign in to sync `settings.json` and `keybindings.json` between machines.
- Settings sync shows its status in the status bar and an account panel.
- `syncIgnored` keeps chosen settings on one machine.
- Settings sync retries on its own and explains errors.
- Conflicting `keybindings.json` changes open side by side to merge.
- Mayoi speaks Simplified Chinese and Japanese (#33).
- Commit comparisons open in a diff tab instead of a patch.
- Right-click the terminal panel's header to move, maximize or hide it.
- Source Control's **Remotes** shows where each remote points.
- Fix "No remotes" flashing when switching back to **Remotes**.
- Commits keep their time visible in a narrow sidebar.
- The terminal panel can sit at the bottom, left or right; see `panelPosition`.
- The Explorer and Source Control tree indent less.
- Linux: fix a startup crash on GPU machines (#24).
- Linux: text is crisp again with subpixel antialiasing.
- Linux: tabs indent to tab stops.
- Linux: submenus of the menu bar open correctly.
- Windows and Linux: shortcuts show as key caps.
- Linux: `install.sh` and the Debian package add the `mayoi` command.
- Opening a branch from another worktree works with Git before 2.36.
- Canceling a remote paste leaves nothing behind.
- Fix Git work from a previous folder landing in the new one's Source Control.
- Source Control's **Repository** section manages branches and remotes.
- Clicking anywhere on an Explorer row opens the file, and arrow keys preview files.
- Review comments: comment on diff or file lines and send them to the terminal.
- A **+** button in diffs adds a review comment.
- Diffs with review comments offer **Send N Comments**.
- Files show review comments after their lines.
- Redesigned review comment cards with an **Outdated** badge.
- Review comments use the window's font on a raised card.
- Review comments are kept with the session.
- `n` and `N` in a diff go between review comments.
- Deleting review comments can be undone.
- Review comments in files show on a tinted chip.
- Lock a column to keep it for terminals.
- Source Control supports multi-select with Shift and Cmd.
- The Explorer and Source Control tree draw indent guides.
- The Markdown preview draws task checkboxes and softer bullets.
- Fitted images have a margin and hairline border.
- Painting allocates about a quarter as much.
- Windows: background git no longer flashes console windows.
- MyGo 0.5.5: system double-click speed, and image paste in terminal programs.
- A commit's menu saves, copies, compares and opens it on GitHub, GitLab or Bitbucket.
- **File → New Window** (⇧⌘N) opens an empty window.
- macOS: drop files on the Dock icon, and use Mayoi from **Open With** (#28).
- Middle-click closes a tab.
- Fix a crash when files leave a scrolled diff.
- **Project Tabs** shows the rail with a single project too.
- The rail always shows project icons; **Project Icons** is gone.
- Remote folders show a server badge on the rail.
- Rail labels use two letters to tell similar names apart.
- **Rename Tab…** and **Tab Color** customize a project's square.
- Drop an image on a project's square, or use **Choose Icon…**, to set its icon.

## 0.9.1

- ⇧⌘E focuses the Explorer.
- Remote SSH: copy, paste, drag, upload and download files between machines.
- Remote SSH: forward ports, including from `localhost` links in a terminal.
- Remote SSH: Explorer folders open faster.
- Remote SSH: saving a file changed on the host asks before overwriting.
- Remote SSH: terminals survive a dropped connection and restart on reconnect.
- Remote SSH: pasting files or a screenshot into a remote terminal uploads them.
- Fix Source Control not showing right after `git init`.
- With Git 2.55+, submodule diffs show their commits again.
- Remote SSH: reconnects automatically after a drop.
- Remote SSH: Connect to Host groups recent hosts and shows progress.
- Remote SSH: the folder picker lists folders by name with `..` and `~`.
- Remote SSH: a disconnected folder's Explorer offers to reconnect.
- Remote SSH: fewer round trips and slower polling in the background.
- **View → Project Icons** shows each project's favicon or logo on the rail.
- Project Icons finds mygo, site, docs and macOS app icons.

## 0.9.0

- Remote SSH: **File → Remote SSH → Connect to Host…** opens a folder on another machine.
- Right-to-left selections extend from where they started.

## 0.8.2

- Selecting text in a terminal copies it; see `terminalCopyOnSelect`.
- Each project keeps its own sidebar and panel layout.
- ⌘V pastes a screenshot into Claude Code in the terminal.
- Fix new files in dot-folders, such as `.changeset`, in Changes.

## 0.8.1

- A redrawn project tab rail, with drag to reorder and folder drop to open.
- A terminal bell or notification marks its project's square.
- An empty folder's Explorer offers **New File…** and **New Folder…**.

## 0.8.0

- The title bar and status bar adapt to narrow windows.
- Inactive tabs no longer lay out every frame.
- **View → Project Tabs** keeps every folder in one window, with a rail.
- The commits menu can merge, rebase, reset, revert and cherry-pick.
- Author initials are centered in their circle.
- Crashes are logged to `crash.log` and the `crashes` folder.

## 0.7.0

- MyGo 0.4.8: shortcuts on non-Latin layouts and faster long files.
- The Qoder theme, with Qoduck on the welcome page and in the status bar.
- Switching to a branch in another worktree offers to open that worktree.
- Better Windows and Linux shortcuts, with VS Code's keys; see `vimCtrlKeys`.
- Custom shortcuts in `keybindings.json`, in VS Code's format.
- New commands to sort lines, camel case, copy or reveal the active file, and set indentation.
- The sidebar's title is no longer cut short.
- Quick open's footer shows its prefixes as keys.
- Destructive buttons such as **Discard** are a deeper red.
- **⇧⌘V** previews Markdown files.
- Sideways trackpad swipes scroll long lines smoothly.

## 0.6.0

- **⌘C** and **⌘V** copy and paste files in the Explorer.
- Double-clicking a file in the Explorer keeps its tab.
- Commits clicked in **COMMITS** open in the preview tab.
- Drag files from the Explorer, Source Control or Search to open them where dropped.
- A commit's tab wraps its message.
- Click the Explorer's empty space to deselect, or right-click it for a workspace menu.

## 0.5.1

- **COMMITS** draws the history as a graph with branches and tags.
- **Show All Branches** shows every branch and tag; see `scmAllBranches`.
- A commit's menu checks it out, creates a branch or tag, and copies names.
- Fix the source control **⋯** menu ignoring clicks past the sidebar.
- A closed folder reopens with its tabs, columns and Explorer state.
- Tabs restore their caret, selection, scroll and word wrap.
- Window state moves from `settings.json` to `state.json`.
- Fix a crash when closing a column's last tab.

## 0.5.0

- The Claude theme, with Clawd on the welcome page and in the status bar.
- **View → Theme** picks the theme and, separately, System, Light or Dark.
- **Search** (⇧⌘F) finds and replaces across files.
- Search results are keyboard-navigable and searches are faster.
- Preview tabs: a single-clicked file opens in an italic tab the next one replaces.
- **File → Open Settings** (⌘,) applies changes on save.
- New font settings, such as `fontWeight`, `fontFeatures`, `lineHeight` and `terminalFontFamily`.
- Zoom resizes terminals in every window.
- **Reveal in Explorer** and **Reveal in Finder** in a tab's menu.
- The welcome page's section titles match the sidebar's.
- Diffs of binary or unchanged files say so in the window's font.

## 0.4.0

- **Go → Command Palette…** (⇧⌘P) runs any command by name.
- Better language detection for `go.mod`, `.sql`, Brewfiles and `.npmrc`.
- The Matrix's welcome page sits on a panel so it reads.
- Line numbers and secondary text are easier to read.

## 0.3.3

- **View → Theme** adds The Matrix.
- Clicking a file or commit in source control focuses its changes.

## 0.3.2

- Drop files from the Finder to open them, or on a terminal to type their paths.
- Primary buttons stay purple on hover and press.
- Quick open, pickers, find and change peek fade in.
- A window without a folder offers to open one in the Explorer.

## 0.3.1

- MyGo 0.4.1: fix select lists jumping and input method highlighting.
- Release files are named `Mayoi.<version>.<arch>.dmg` and `Mayoi.Setup.<version>.<arch>.exe`.

## 0.3.0

- **View → Vim Mode** edits with vim's keys.
- **View → Sidebar on Right** moves the sidebar.

## 0.2.1

- Source control lists the branch's recent commits.
- The commit button says what it will do, and adds **Undo Last Commit**.
- **Switch Branch…** finds, switches and creates branches.
- **View as Tree** lists changes under their folders.
- A clean work tree says so and offers to push or pull.
- The commit summary shows its length past 50 characters.
- A progress bar and toasts show Git commands' progress.
- Stage, unstage or discard one change at a time.
- A file's header sticks to the top of the diff.
- Diff headers show icon, state, rename source and line counts.
- Source control stays fast with thousands of changes.
- **File → Open Recent** lists recent folders, and ⌃R finds one.

## 0.2.0

- Source control: stage, unstage, discard and commit.
- Diffs open in a tab, inline or side by side.
- Diffs fold unchanged lines and wrap long ones.
- One **Changes** tab shows every changed file.
- Pager keys move around a diff.
- Click a gutter mark to see, revert or stage a change.
- The Explorer shows Git status colors.
- The status bar's branch switches, fetches, pulls and pushes.
- The caret's line shows who changed it last.
- ⇧⌘T reopens the last closed tab.
- Highlighting keeps up with typing in large files.
- Large files highlight the visible lines first.
- The tab strip has a bottom edge.
- Readability tweaks to line numbers, find fields and the status bar.

## 0.1.0

- The first release: tabs, columns, file tree, quick open, find, Git marks, terminal and auto save.
- Mayoi updates itself from GitHub Releases.
