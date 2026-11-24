import { commands, events, log, settings, storage, views, window, workspace, type ViewNode, type ViewTreeItem, type WorkspaceContext } from "mayoi";

interface Todo {
  file: string;
  line: number;
  tag: string;
  text: string;
  searchable: string;
}

const maxFiles = 4000;
const maxSize = 512 * 1024;
const viewId = "todos.list";

interface ScanScope {
  include: string;
  exclude: string;
}

interface TodoView {
  context: WorkspaceContext;
  scope: ScanScope;
  draft: ScanScope;
  details: boolean;
  filter: string;
  filterTimer?: ReturnType<typeof setTimeout>;
  todos: Todo[];
  loading: boolean;
  failed: boolean;
  scanId: number;
}

const openViews = new Map<number, TodoView>();

function commaList(value: string): string[] {
  return value
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

function scopeKey(context: WorkspaceContext): string {
  return "scope:" + context.root;
}

function show(context: WorkspaceContext) {
  if (!context.root) return;
  const existing = openViews.get(context.windowId);
  if (existing?.context.root === context.root) {
    if (!existing.loading) return scan(existing);
    return;
  }
  if (existing) {
    existing.scanId++;
    clearTimeout(existing.filterTimer);
  }
  const saved = storage.get<Partial<ScanScope>>(scopeKey(context));
  const scope = {
    include: typeof saved?.include === "string" ? saved.include : "",
    exclude: typeof saved?.exclude === "string" ? saved.exclude : "",
  };
  const view: TodoView = {
    context,
    scope,
    draft: { ...scope },
    details: false,
    filter: "",
    todos: [],
    loading: false,
    failed: false,
    scanId: 0,
  };
  openViews.set(context.windowId, view);
  return scan(view);
}

async function applyScope(view: TodoView) {
  const scope = { ...view.draft };
  if (scope.include !== view.scope.include || scope.exclude !== view.scope.exclude) {
    try {
      storage.set(scopeKey(view.context), scope);
    } catch (err) {
      log.error(err);
      await window.fail("Could not save the TODOs scan scope.");
      return;
    }
    view.scope = scope;
  }
  await scan(view);
}

async function scan(view: TodoView) {
  const id = ++view.scanId;
  view.loading = true;
  view.failed = false;
  view.todos = [];
  const context = view.context;
  const scope = view.scope;
  try {
    await render(view);
    const want = commaList(String(settings.get("tags") || "TODO"));
    // \b only holds next to a letter or digit, so a tag like [TODO] or 待办 goes without it.
    const one = (t: string) => (/^\w/.test(t) ? "\\b" : "") + t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + (/\w$/.test(t) ? "\\b" : "");
    const re = new RegExp(`(${want.map(one).join("|")})[:\\s(]?(.*)$`);
    const files = await workspace.findFiles(scope.include.trim(), {
      ...context,
      limit: maxFiles,
      followSymlinks: true,
      exclude: scope.exclude,
      excludeFolders: commaList(String(settings.get("exclude") || "")),
    });
    const found: Todo[] = [];
    for (const file of files) {
      if (id !== view.scanId) return;
      const st = await workspace.stat(file, context);
      if (!st || st.isDir || st.size > maxSize) continue;
      let text: string;
      try {
        text = await workspace.readFile(file, context);
      } catch {
        continue;
      }
      if (text.includes("\u0000")) continue;
      const lines = text.split("\n");
      for (let i = 0; i < lines.length; i++) {
        const m = re.exec(lines[i]);
        if (m) {
          const tag = m[1];
          const content = m[2].replace(/\*\/\s*$/, "").trim();
          found.push({ file, line: i + 1, tag, text: content, searchable: `${file}\n${tag}\n${content}`.toLowerCase() });
        }
      }
    }
    if (id === view.scanId) view.todos = found;
  } catch (err) {
    if (id === view.scanId) {
      view.failed = true;
      log.error(err);
    }
  } finally {
    if (id === view.scanId) {
      view.loading = false;
      await render(view);
    }
  }
}

function scopeField(view: TodoView, field: keyof ScanScope, label: string, example: string): ViewNode {
  return {
    type: "stack",
    gap: 3,
    children: [
      { type: "text", text: label, muted: true },
      {
        type: "input",
        value: view.draft[field],
        label: "Files to " + field,
        placeholder: example,
        onChange(value) { view.draft[field] = value; },
        onSubmit(value) {
          view.draft[field] = value;
          return applyScope(view);
        },
      },
    ],
  };
}

function render(view: TodoView) {
  const terms = view.filter.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const byFile = new Map<string, Todo[]>();
  let count = 0;
  for (const t of view.todos) {
    if (!terms.every((term) => t.searchable.includes(term))) continue;
    if (!byFile.has(t.file)) byFile.set(t.file, []);
    byFile.get(t.file)!.push(t);
    count++;
  }
  const items: ViewTreeItem[] = [...byFile].map(([file, todos]) => ({
    key: terms.length ? `filtered:${file}` : file,
    label: file.split("/").pop()!,
    description: `${todos.length}  ${file.includes("/") ? file.slice(0, file.lastIndexOf("/")) : ""}`,
    icon: "file",
    open: true,
    children: todos.map((t) => ({
      key: `${file}:${t.line}`,
      label: t.text || t.tag,
      description: `${t.tag} · ${t.line}`,
      icon: t.tag.replace(/\W/g, "") === "FIXME" ? "warning" : "check",
      onClick: () => workspace.open(t.file, { ...view.context, line: t.line }),
    })),
  }));
  let results: ViewNode;
  if (view.loading) {
    results = { type: "row", children: [{ type: "progress" }, { type: "text", text: "Finding TODOs…", muted: true }] };
  } else if (view.failed) {
    results = { type: "empty", icon: "warning", text: "Could not scan TODOs." };
  } else if (items.length) {
    results = { type: "tree", items };
  } else {
    results = { type: "empty", icon: view.todos.length ? "search" : "check", text: view.todos.length ? "No matching TODOs." : "No TODOs in this folder." };
  }
  return views.render(viewId, {
    type: "stack",
    children: [
      {
        type: "input",
        placeholder: "Filter TODOs…",
        onChange(value) {
          view.filter = value;
          clearTimeout(view.filterTimer);
          view.filterTimer = setTimeout(() => void render(view), 120);
        },
      },
      {
        type: "row",
        children: [
          { type: "text", text: `${count} in ${byFile.size} files`, muted: true },
          { type: "button", label: "Refresh", onClick: () => applyScope(view) },
          {
            type: "button", label: "⋯", tooltip: "Scan scope",
            onClick() {
              view.details = !view.details;
              return render(view);
            },
          },
        ],
      },
      {
        type: "stack",
        children: view.details ? [
          scopeField(view, "include", "Include", "src/**"),
          scopeField(view, "exclude", "Exclude", "**/*.test.ts, dist/**"),
        ] : [],
      },
      results,
    ],
  }, view.context);
}

views.onShow(viewId, show);
commands.register("todos.refresh", (_arg, context) => views.reveal(viewId, context));
events.on("workspace.changed", (context) => {
  const view = openViews.get(context.windowId);
  if (view && view.context.root !== context.root) {
    if (context.root) return show(context);
    view.scanId++;
    clearTimeout(view.filterTimer);
    openViews.delete(context.windowId);
  }
});
events.on("editor.didSave", ({ document }) => {
  const view = openViews.get(document.ref.windowId);
  if (view && !view.loading) return scan(view);
});
