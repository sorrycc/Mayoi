# Plugins

Plugins add commands, react to the editor, check and format code, draw views and previews, and bring themes, languages,
snippets and language servers. They are folders of JavaScript or TypeScript that Mayoi runs in its own engine,
[moejs](https://github.com/MoeForge/moejs): no Node, no build step, and no webview unless a plugin asks for one.

The API is new and may still change before 1.0. A plugin says which versions of Mayoi it works with in `mayoi`.

## Using plugins

### Installing

```sh
mayoi plugin install mayoi-editor/plugin-prettier         # owner/repo of GitHub
mayoi plugin install https://git.example.com/me/tool.git@v1.2.0
mayoi plugin install prettier                              # a name of the index
mayoi plugin list
mayoi plugin update                                        # all, or name...
mayoi plugin remove prettier
```

Or choose **Plugins → Manage Plugins…**, or the **Plugins** page of the settings window: install from a Git URL or
the index, update, remove, turn each plugin on and off, and change its settings.

The `mayoi plugin` command works on the plugins folder, so it works with Mayoi closed; a Mayoi that runs loads what
changed at once.

Plugins are trusted as settings are: installing one means trusting its code, which runs with your rights. Install
plugins you know.

### Where they are

| Folder                              | What                                                        |
| ----------------------------------- | ----------------------------------------------------------- |
| `~/.mayoi/plugins/<name>/`          | Installed plugins, a folder each                            |
| `~/.mayoi/plugins/plugins.json`     | Where each plugin installed from Git came from, and its commit |
| `~/.mayoi/scripts/`                 | User scripts                                                |
| `~/.mayoi/storage/<name>/`          | What plugins store                                          |

On Windows, `~` is `%USERPROFILE%`. With `MAYOI_DATA_DIR` set, these folders are in that folder instead.

Mayoi comes with two plugins of its own, **Word Count** (the words and reading time of a Markdown or text file, in the
status bar) and **TODOs** (the folder's TODO, FIXME and HACK comments, in the sidebar). They start off; turn them on
on the **Plugins** page of the settings window, or with `"enabled": true` in their settings.

### Settings sync

Signed in, the plugins installed from Git follow you to your other machines: Mayoi installs them there at the same
commits, and a plugin removed on one machine is removed on the others. A plugin copied in by hand, or linked with
`mayoi plugin dev`, stays on its machine.

### When something goes wrong

- **Plugins → Show Plugin Log** opens a tab of what plugins logged and the errors they hit, the newest at the bottom.
  Filter it by plugin, by level and by text; it follows new lines while scrolled to the end. **Copy** copies the lines
  shown, and **Clear** empties it. It keeps the last 10,000 lines.
- **Plugins → Show Running Plugins** opens a table of the plugins: state, version, the time their JavaScript ran, the
  memory they keep, when they started, how often they crashed, and their last error. Click a column's head to sort by
  it. It refreshes every second while it shows. Each row can restart its plugin, turn it off, open its details, or
  show its log.
- A command's JavaScript that runs for 10 seconds without a break is stopped, and so is a plugin that keeps more than
  256 MiB. A plugin that stops 3 times in a row is off until **Plugins → Reload Plugins**.
- `mayoi --disable-plugins` starts Mayoi with only the plugins it comes with.

### Settings of plugins

```jsonc
{
  "plugins": {
    "word-count": { "enabled": true, "wordsPerMinute": 200 },
    "prettier": { "enabled": false }
  },
  "formatOnSave": { "typescript": true },
  "formatters": { "typescript": "prettier" }
}
```

See [SETTINGS.md](SETTINGS.md#plugins).

## Writing a plugin

```sh
mayoi plugin new word-stats   # writes ./word-stats
cd word-stats
mayoi plugin dev              # loads it, and again on each save
```

`mayoi plugin new` writes `plugin.json`, `main.ts`, `mayoi.d.ts` (the types of the API, for your editor) and an
`AGENTS.md` that tells a coding agent how plugins work: with no build step and a reload on each save, an agent can
write a plugin and see it work at once.

### A plugin's folder

```
word-stats/
  plugin.json
  main.ts
  mayoi.d.ts
```

### plugin.json

```jsonc
{
  "name": "word-stats",              // lowercase letters, digits and hyphens; the prefix of its ids
  "displayName": "Word Stats",
  "version": "0.1.0",
  "description": "Words of the selection",
  "author": "Ann",                   // optional, shown on the Plugins page
  "mayoi": ">=0.19.0",               // the versions of Mayoi it works with
  "main": "main.ts",                 // .ts, .js or .mjs
  "activation": ["onCommand:word-stats.count"],
  "enabled": true,                   // optional: false starts it off, until its settings turn it on
  "contributes": {
    "commands": [{ "id": "word-stats.count", "title": "Count Words", "title:zh-Hans": "统计字数" }],
    "keybindings": [{ "key": "ctrl+alt+w", "mac": "cmd+alt+w", "command": "word-stats.count" }],
    "menus": { "plugins": [{ "command": "word-stats.count" }] },
    "settings": { "wordsPerMinute": { "type": "number", "default": 230, "description": "Reading speed" } }
  }
}
```

Every id starts with the plugin's name and a dot. A title takes other languages as `title:<tag>`, for
**View → Language**. Mayoi's own prefixes, as `file`, `edit`, `view` and `git`, are reserved.

#### Activation

A plugin starts when something it names happens, and costs nothing before:

| Event                       | When                                                    |
| --------------------------- | ------------------------------------------------------- |
| `onCommand:<id>`            | One of its commands runs. Any command starts its plugin. |
| `onLanguage:<language>`     | A document of a language opens or comes to the front     |
| `workspaceContains:<glob>`  | The folder has a file the glob matches, as `go.mod`     |
| `onView:<id>`               | One of its views shows                                  |
| `onPreview:<id>`            | One of its previews shows                               |
| `onStartup`                 | Mayoi starts                                            |
| `*`                         | Anything above                                          |

Languages are the highlighter's names in lower case: `go`, `typescript`, `markdown`, `python`, `plaintext`, and the
languages plugins declare.

#### Contributions

| Key               | What it adds                                                                                   |
| ----------------- | ---------------------------------------------------------------------------------------------- |
| `commands`        | Commands in the command palette, the shortcuts editor and `mayoi run`. `arg` names what `mayoi run id=arg` gives. |
| `keybindings`     | Shortcuts in keybindings.json's format. A plugin only gets keys that are free; see below.      |
| `menus`           | `plugins` (the Plugins menu), `explorer/context` and `tab/context`, whose commands get the path |
| `settings`        | Settings under `plugins.<name>` in settings.json, edited on the Plugins page: `type` is `boolean`, `number` or `string`, with `enum` for a choice |
| `statusItems`     | Items of the status bar, `align` `left` or `right`, which the plugin fills                     |
| `views`           | Views in the sidebar, beside Explorer, Search and Source Control, which the plugin renders     |
| `previews`        | Previews beside the source of the files a `pattern` matches, `output` `svg`, `markdown` or `view` |
| `formatters`      | Formatters of `languages`, for **Format Document** and `formatOnSave`                          |
| `webviews`        | Panels of HTML from the plugin's folder, in the sidebar                                        |
| `themes`          | Themes, in Mayoi's format or VS Code's                                                         |
| `languages`       | File `extensions` and `filenames`, the Chroma `lexer` that highlights them, and `lineComment`  |
| `snippets`        | Snippets in VS Code's format, for a `language` or `*`                                          |
| `terminalLinks`   | A `pattern` of links clicked in a terminal (URLs and paths), which runs `command` with the match |
| `languageServers` | Language servers: a `command`, its `args` and `initializationOptions`, for `languages`         |
| `agents`          | Coding agents whose sessions the **Sessions** view lists and resumes; see below                 |

#### Keyboard shortcuts

A plugin's shortcut takes a key only while no command of Mayoi's has it by default and keybindings.json does not bind
it, so installing a plugin never changes what a key you know does. A shortcut that is taken shows on the Plugins page
of the settings window, where **Use Plugin's Shortcut** writes it to keybindings.json. keybindings.json can bind any
plugin command to any key, as a Mayoi command.

### main.ts

```ts
import { commands, editor, settings, window } from "mayoi";

commands.register("word-stats.count", async () => {
  const ed = await editor.active();
  if (!ed) return;
  const s = ed.selection;
  const text = await ed.document.text({ start: s.anchor, end: s.active });
  const words = text.split(/\s+/).filter(Boolean).length;
  await window.notify(`${words} words, ${Math.ceil(words / settings.get<number>("wordsPerMinute"))} min`);
});
```

A plugin may also export `activate()`, which runs as it starts, after its top level.

What a plugin's code has:

- The `mayoi` module, below. Its types are in `mayoi.d.ts`.
- `console`, whose lines go to the plugin's log.
- `fetch`, `Request`, `Response`, `Headers`, `URL`, `URLSearchParams`, the Streams, `AbortController`, `setTimeout`,
  `setInterval` and `queueMicrotask`.
- Imports of its own files by relative path, with or without their extension.

What it does not have: `require`, `node:` modules, npm packages at run time, `eval` and `new Function`. To use a
package, `npm install` it in the plugin's folder and run `mayoi plugin pack`, which copies the files of the ES module
build it imports into `vendor/` and points the imports at them. A package that is only CommonJS cannot run.

A file larger than 1 MiB is not loaded.

### The mayoi module

Positions are a `line` and a `character` in UTF-16 code units, both from 0, as JavaScript strings and LSP count them.
Every call that reaches the editor returns a promise.

| Namespace                | Calls                                                                                     |
| ------------------------ | ----------------------------------------------------------------------------------------- |
| `commands`               | `register(id, fn, {title})`, `run(id, arg)` (Mayoi's commands too), `list()`              |
| `editor`                 | `active()`, `visible()`; an editor's `document`, `selections`, `edit(edits, {version})`, `insert(text)`, `setSelections`, `reveal(line)` |
| a document               | `path`, `name`, `language`, `version`, `isDirty`, `lineCount`, `text(range?)`, `lineAt(line)`, `save()` |
| `workspace`              | `root()`, `isRemote()`, `open(path, {line})`, `readFile`, `writeFile`, `stat`, `findFiles(glob)`, `documents()` |
| `window`                 | `notify(message, {action})`, `fail`, `pick(items)`, `prompt({title, value})`, `progress(title, fn)`, `openText(text)` |
| `settings`               | `get(key)` of the plugin's settings, `all()`, `mayoi(key)` of Mayoi's own, `onChange(fn)` |
| `storage`                | `get(key)`, `set(key, value)` and `keys()`, global or with `{folder: true}` of the folder |
| `events`                 | `on(name, fn)`; see below                                                                 |
| `process`                | `run(command, args, {cwd, input, timeout})`, on the folder's machine                      |
| `languages`              | `setDiagnostics(path, items)`, `clearDiagnostics(path?)`, `registerFormatter(id, fn)`, `registerHover(language, fn)` |
| `statusItem(id)`         | `set({text, tooltip, icon, command, onClick})`, `hide()`                                  |
| `views`                  | `render(id, tree)`, `onShow(id, fn)`, `reveal(id)`                                        |
| `previews`               | `register(id, fn)`, `refresh(id)`                                                         |
| `webview`                | `open(id)`, then the panel's `post(message)`, `onMessage(fn)`, `close()`                  |
| `agents`                 | `register(id, fn)`, the function that lists the sessions of an agent                      |
| `ai`                     | `complete(prompt, {system})`, with Mon's AI gateway while you are signed in               |
| `log`                    | `info`, `warn`, `error`, `debug`                                                          |

An edit names the version of the document it was worked out from. When the document changed since, as when you typed
meanwhile, `edit` returns `false` and changes nothing. An edit that applies is one step of undo.

#### Events

| Event               | When                                              |
| ------------------- | ------------------------------------------------- |
| `editor.open`       | A document opens                                  |
| `editor.change`     | A document changed, once its edits rest for 0.3 s |
| `editor.focus`      | A document comes to the front                     |
| `editor.didSave`    | A document was saved, by hand or by auto save     |
| `editor.close`      | A document closed                                 |
| `workspace.changed` | The window's folder changed                       |
| `settings.changed`  | The settings changed                              |
| `theme.changed`     | The theme or the appearance changed               |

A document event has the document's `path`, `language`, `version`, and the `document` itself.

### Files and programs on SSH hosts

Plugins run on this machine. `workspace` and `process` reach the folder's machine, so `readFile("go.mod")` reads it on
the host and `process.run("gofmt", ["-l", "."])` runs there, in the folder, with nothing installed on the host for
the plugin. Each call takes a round trip there.

### Diagnostics

```ts
import { events, languages, process } from "mayoi";

events.on("editor.didSave", async (e) => {
  if (e.language !== "go") return;
  const { stdout } = await process.run("go", ["vet", "./..."]);
  // ... parse it, then:
  languages.setDiagnostics(e.path, [
    { range: { start: { line: 4, character: 1 }, end: { line: 4, character: 9 } }, severity: "warning", message: "…" },
  ]);
});
```

Diagnostics are underlined in the editor, as their severity: error, warning, info or hint. The status bar counts the
errors and warnings, a click shows them all in **Show Problems**, and the message of the one on the caret's line shows
beside it.

### Formatters

```jsonc
"formatters": [{ "id": "prettier.format", "languages": ["typescript", "javascript", "css"] }]
```

```ts
languages.registerFormatter("prettier.format", async (doc, { tabSize, insertSpaces }) => {
  const { stdout, code } = await process.run("npx", ["prettier", "--stdin-filepath", doc.path], { input: await doc.text() });
  return code === 0 ? stdout : null; // the whole new text, or edits, or null
});
```

**Selection → Format Document** (⇧⌥F) formats with it. With `formatOnSave` on for the language, saving by hand formats
first, for up to 1.5 seconds; past that, the file saves as it is. Auto save does not format.

### Views

A view is a tree of nodes that Mayoi draws in its theme, the zoom and the language, with no webview:

```ts
views.onShow("todo.list", () =>
  views.render("todo.list", {
    type: "list",
    items: todos.map((t) => ({
      key: t.id, label: t.title, description: t.file, icon: "check",
      onClick: () => workspace.open(t.file, { line: t.line }),
      actions: [{ icon: "trash", tooltip: "Remove", onClick: () => remove(t.id) }],
    })),
  })
);
```

Nodes: `stack` and `row` with `children`, `text`, `markdown`, `code`, `svg`, `list`, `tree` (items with `children`,
`open` at first), `input` (`onChange`, `onSubmit`), `select`, `checkbox`, `button`, `progress` (a `value` from 0 to 1,
or a spinner without one), `divider` and `empty` (its `text` under an `icon`). A function in the tree, as `onClick`, is
called back in the plugin.
Icons are Mayoi's: `check`, `x`, `plus`, `file`, `folder`, `search`, `star`, `info`, `warning`, `error`, `trash`,
`refresh`, `settings`, `terminal`, `branch`, `commit`, `tag`, `clock`, `lightbulb`, `sparkles`, `comment`, `copy`,
`external`, `pencil`, `list`, `function`, `hash`, `lock`, `server`, `cloud`, `send`, `keyboard`, `box`, `braces`,
`download` and `image`.

### Previews

```jsonc
"previews": [{ "id": "d2.preview", "pattern": "*.d2", "output": "svg" }]
```

```ts
previews.register("d2.preview", async (text, { path }) => {
  const { stdout } = await process.run("d2", ["-", "-"], { input: text });
  return stdout; // SVG
});
```

**View → Toggle Preview** (⇧⌘V) and **Open Preview to the Side** show it, rendered again a moment after each edit.
Output `markdown` returns Markdown text, and `view` a tree of nodes.

### Webviews

For what nodes cannot draw, a webview shows an HTML file of the plugin's folder in the sidebar:

```jsonc
"webviews": [{ "id": "chart.panel", "title": "Chart", "path": "panel.html" }]
```

```ts
const panel = await webview.open("chart.panel");
panel.onMessage((m) => log.info("from the page", m));
panel.post({ points: [1, 2, 3] });
```

The page has `mayoi.post(message)` and `mayoi.onMessage(fn)`. On Linux, webviews need WebKitGTK.

### Themes

```jsonc
"themes": [{ "id": "night.dark", "label": "Night", "path": "night.json" }]
```

A theme file is Mayoi's format, the colors of one of Mayoi's palettes changed:

```json
{
  "base": "dark",
  "colors": { "editor": "#1e1f29", "chrome": "#191a21", "accent": "#bd93f9", "selection": "#44475a80" },
  "syntax": { "keyword": "#ff79c6", "string": "#f1fa8c", "comment": "#6272a4" },
  "ansi": ["#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2"]
}
```

or a VS Code color theme as it is, whose `colors` and `tokenColors` Mayoi maps to its own. `base` is `dark`, `light`
or a theme's id, as `claude`. The theme shows in **View → Theme → Color Theme…**, the settings window, and the command
palette.

### Languages and snippets

```jsonc
"languages": [{ "id": "d2", "extensions": [".d2"], "lexer": "yaml", "lineComment": "#" }],
"snippets": [{ "language": "d2", "path": "snippets.json" }]
```

`lexer` is a [Chroma](https://github.com/alecthomas/chroma) lexer's name. Snippets are VS Code's format, with tab stops,
placeholders, choices and variables as `TM_FILENAME` and `CURRENT_YEAR`; **Selection → Insert Snippet…** inserts one.

### Language servers

```jsonc
"languageServers": [{ "id": "gopls.server", "languages": ["go"], "command": "gopls" }]
```

A plugin of a few lines brings a language: Mayoi starts the server as a document of its languages opens, on the
folder's machine, so it runs on the SSH host of a remote folder. Mayoi shows the server's diagnostics, and asks it for
**Go → Go to Definition** (F12), **Go → Show Hover**, **Selection → Complete Word** (⌃Space in the editor), and
**Format Document** when no plugin formats the language.

For servers that need setup, use `resolve` instead of `command`:

```jsonc
"settings": { "helperPath": { "type": "string", "default": "my-helper" } },
"languageServers": [{
  "id": "example.server", "languages": ["java"],
  "resolve": { "command": "${settings.helperPath}", "args": ["resolve"] }
}]
```

The resolver runs once at server startup, on the workspace's machine. Its stdin
is JSON `{root, settings}`: an absolute host path and the plugin's explicitly
stored settings (without manifest defaults). `${settings.key}` references in the
resolver command and arguments use stored settings with manifest defaults as a
fallback; substitution passes literal arguments and never invokes a shell.
Keep resolution fast and offline; dependency installation belongs in a separate
setup command. Resolution and initialization share a 30-second deadline.

The resolver must print one JSON object and exit successfully:

```ts
{
  command: string;
  args?: string[];
  env?: Record<string, string>;
  unsetEnv?: string[];
  initializationOptions?: unknown;
  configuration?: Record<string, unknown>;
  workspaceFolders?: { uri: string; name: string }[];
  pathMappings?: { clientURI: string; serverURI: string }[];
}
```

Mayoi starts the returned command directly. `env` overrides inherited variables;
`unsetEnv` removes them on the workspace host. `configuration` answers
`workspace/configuration` requests by dotted section. `workspaceFolders` is used
for initialization and folder requests; the first folder supplies `rootUri`.
Folders and mappings must use absolute `file:` URIs. URI prefix mappings run in
both directions and match whole path components, longest first. Mapping follows
known LSP document, workspace, definition and diagnostic URI fields, including
keys in workspace edits. Source/edit text, hover content, configuration,
`initializationOptions` and unknown extension payloads pass through unchanged.
Plugins prepare server-specific paths inside their own configuration. This lets
a server import canonical paths and the editor keep the workspace's symlink
aliases for definitions and diagnostics. Standard declarations remain valid.

### Coding agents

The **Sessions** view lists the sessions of Claude Code, Qoder, Codex, Grok and Pi. A plugin adds another agent, whose
sessions show there with its icon and resume in a terminal tab as theirs do:

```jsonc
"agents": [{
  "id": "pi.agent", "name": "Pi",
  "icon": "sparkles",                      // one of the icons of views, or an SVG: "<path d=\"…\"/>" of a 24×24 view
  "tint": ["#e0559b", "#b8326f"],          // optional: one hex color, or one for dark windows and one for light
  "resume": "pi --resume {id}",            // run in the session's folder; {id}, {folder} and {title} are quoted
  "sessions": {                            // optional: a session for each file the glob matches
    "glob": "~/.pi/sessions/**/*.jsonl", "format": "jsonl",
    "id": "sessionId", "title": "title", "folder": "cwd", "updated": "timestamp", "prompt": "message.content"
  }
}]
```

`sessions` reads each file the glob matches (`~` is the home folder, `**` any folders) as `json`, one object, or
`jsonl`, an object a line. Each field is a path of keys by dots, as `meta.cwd` or `messages.0.content`; of a `jsonl`
file each takes the first line that has it, but `title` and `updated` the last. A session's id is the file's name
without its extension unless `id` names one, and its time the file's unless `updated` names one: milliseconds or
seconds since 1970, or ISO 8601 text. A file is read again only once it changes.

An agent without `sessions` lists them with a function, which starts the plugin as the view reads:

```ts
import { agents, workspace } from "mayoi";

agents.register("pi.agent", async () => {
  const index = JSON.parse(await workspace.readFile("/Users/me/.pi/index.json"));
  return index.map((s) => ({ id: s.id, title: s.name, folder: s.cwd, updated: s.mtime, prompt: s.firstPrompt }));
});
```

Mayoi calls it off its main thread each time it reads the sessions, every few seconds while the view shows, and waits
1.5 seconds for it: what comes later shows a moment after, and a function that runs 10 seconds is given up. One that
fails keeps the sessions it gave last, and its error goes to the plugin's log, once until it changes. A plugin turned
off takes its agents and their sessions with it.

### User scripts

A `.ts` or `.js` file in `~/.mayoi/scripts` is a plugin without a manifest: each function it exports is a command,
`script.<file>.<function>`, titled from its name, in the command palette, the shortcuts editor and `mayoi run`. A
function gets what `mayoi run script.text.upperCase=hello` gives, or nothing from the palette.
**Plugins → Open Scripts Folder** makes the folder with an example.

**Plugins → Run Selection as Script** runs the selection, or the whole file, once, as a plugin of its own: its
`console.log` goes to the log, and what its default export returns shows in a toast.

## Sharing a plugin

Push the folder to a Git repository: `mayoi plugin install owner/repo` installs it. To list it in the index, add it to
`index.json` of [mayoi-editor/plugins](https://github.com/mayoi-editor/plugins) by pull request:

```json
{ "name": "word-stats", "description": "Words of the selection", "author": "Ann", "repository": "https://github.com/me/word-stats", "tags": ["text"] }
```
