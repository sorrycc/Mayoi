# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## Unreleased

- **View → Preview Markdown on Open** opens Markdown and `.mdx` in preview mode.
- `⇧⌘V` shows source; new files, search results and line jumps open as source.
- Restored and reopened Markdown tabs keep their view and scroll position, including offline remote tabs.
- MDX previews omit top-level `import`/`export` blocks, preserving code examples and paragraph text.
- `mayoi run <id>[=<arg>]` runs any command of the window, and `mayoi commands` lists them.
- `mayoi -g file:line` opens a file at a line, `-d` compares two files, and `-n` opens a new window.
- `mayoi --remote host:/path` opens a folder on an SSH host.
- `mayoi -w` waits for its files to close, so it works as `git config core.editor "mayoi -w"`.
- `mayoi` says what went wrong and exits non-zero when it fails.
- Mayoi updates a `mayoi` command installed by an older version as it starts.
- Opened from the Finder or Dock, Mayoi uses your shell's `PATH`, so git hooks find `pnpm` and `node`.

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
