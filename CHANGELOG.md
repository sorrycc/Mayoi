# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## Unreleased

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
