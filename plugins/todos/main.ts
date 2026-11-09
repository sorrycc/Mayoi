import { commands, events, settings, views, workspace, type ViewTreeItem } from "mayoi";

interface Todo {
  file: string;
  line: number;
  tag: string;
  text: string;
}

const maxFiles = 4000;
const maxSize = 512 * 1024;

function tags(): string[] {
  return String(settings.get("tags") || "TODO")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
}

function excluded(file: string): boolean {
  const dirs = String(settings.get("exclude") || "")
    .split(",")
    .map((d) => d.trim())
    .filter(Boolean);
  return dirs.some((d) => file === d || file.startsWith(d + "/") || file.includes("/" + d + "/"));
}

let scanning = false;

async function scan() {
  if (scanning) return;
  scanning = true;
  try {
    await views.render("todos.list", { type: "row", children: [{ type: "progress" }, { type: "text", text: "Finding TODOs…", muted: true }] });
    const want = tags();
    const re = new RegExp(`\\b(${want.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})\\b[:\\s(]?(.*)$`);
    const files = (await workspace.findFiles("", { limit: maxFiles })).filter((f) => !excluded(f));
    const found: Todo[] = [];
    for (const file of files) {
      const st = await workspace.stat(file);
      if (!st || st.isDir || st.size > maxSize) continue;
      let text: string;
      try {
        text = await workspace.readFile(file);
      } catch {
        continue;
      }
      if (text.includes("\u0000")) continue;
      const lines = text.split("\n");
      for (let i = 0; i < lines.length; i++) {
        const m = re.exec(lines[i]);
        if (m) found.push({ file, line: i + 1, tag: m[1], text: m[2].replace(/\*\/\s*$/, "").trim() });
      }
    }
    await render(found);
  } finally {
    scanning = false;
  }
}

async function render(found: Todo[]) {
  if (found.length === 0) {
    await views.render("todos.list", { type: "empty", icon: "check", text: "No TODOs in this folder." });
    return;
  }
  const byFile = new Map<string, Todo[]>();
  for (const t of found) {
    if (!byFile.has(t.file)) byFile.set(t.file, []);
    byFile.get(t.file)!.push(t);
  }
  const items: ViewTreeItem[] = [...byFile].map(([file, todos]) => ({
    key: file,
    label: file.split("/").pop()!,
    description: `${todos.length}  ${file.includes("/") ? file.slice(0, file.lastIndexOf("/")) : ""}`,
    icon: "file",
    open: true,
    children: todos.map((t) => ({
      key: `${file}:${t.line}`,
      label: t.text || t.tag,
      description: `${t.tag} · ${t.line}`,
      icon: t.tag === "FIXME" ? "warning" : "check",
      onClick: () => workspace.open(t.file, { line: t.line }),
    })),
  }));
  await views.render("todos.list", {
    type: "stack",
    children: [
      {
        type: "row",
        children: [
          { type: "text", text: `${found.length} in ${byFile.size} files`, muted: true },
          { type: "button", label: "Refresh", onClick: scan },
        ],
      },
      { type: "tree", items },
    ],
  });
}

views.onShow("todos.list", scan);
commands.register("todos.refresh", scan);
events.on("editor.didSave", () => {
  if (!scanning) scan();
});
