# Changelog

The update window shows the section of the version it offers. Every release
needs a `## <version>` section here, or its build fails.

## Unreleased

- A big file that changed on disk since it was opened asks about its save: overwrite it, or take the file's text again.
- A big file can be saved to a new path, or again after its file was removed from the disk.
- Typing while a big file saves in the background no longer makes its next save fail.
- A file being written to keeps its text in the editor while it is read again, rather than show the loading view.
- Local files past half a gigabyte open in windows of an index of their lines, so their whole text is never held in memory.
- Closing a tab or switching project stops a whole-file read under way.
- A document too large for a language server is no longer sent to it whole on every change.
## 0.23.3

- Opening a folder with `mayoi <folder>` brings its project window to the front.
- Clicking a file's “Could not read” page or **Try Again** now makes that tab the active one.
- In Live Preview, `Up` from a heading now moves to the line above instead of staying on the heading.
- **Sessions** no longer lists Pi subagent sessions such as `general-purpose#3c8c2fa2`; Pi forks still show.
- **Download** and **Upload** of remote files and folders now work on Windows and in WSL.
- Dragging a file in the explorer no longer stops when you scroll with the wheel.
- Plugins can compare two open buffers, including unsaved tabs, in the native Diff view.
- Buffer comparisons follow edits and keep the last text when a source tab closes.
- Edit either live source directly in a buffer comparison, including unsaved tabs.
- Plugins can supply syntax rules for their languages.
- Plugins can change a document's language without editing its text.
- Plugins can open reusable text output beside its source document.
- Plugin output stays to the right of its source when columns are stacked.
- Plugin text output uses plain text when its language is not recognized.
- Plugins can show clickable actions above source lines, with stale actions removed on edits or reload.
- Plugin text output can target its source window and display JSON with syntax highlighting.
- Plugins keep targeting their source documents when you switch windows.
- Plugins can read a document's text and version together and track untitled documents closing.
- Plugin tab actions receive the tab you chose, including unsaved buffers.
- Right-click a local branch in Source Control and choose **Pull Branch** to update it in place.
- Pulling an inactive branch uses its upstream even if another fetch runs at the same time.
- Local branches checked out in another worktree show a worktree icon and a folder tooltip.
- Hover over a worktree branch to open its checkout in a new window.
- Branches behind their upstream are highlighted and show a pull shortcut on hover.
- Branches ahead of their upstream are highlighted and show a push shortcut on hover.
- Branches ahead or behind their upstream show both commit counts, including zero.
- Click the file's name in the breadcrumbs to list the file's symbols, in its own order.
