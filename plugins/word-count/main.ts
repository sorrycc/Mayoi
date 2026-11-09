import { commands, editor, events, settings, statusItem, window, type Document } from "mayoi";

const status = statusItem("word-count.status");
const languages = new Set(["markdown", "plaintext"]);

// count counts words: runs of letters and digits, and each CJK character,
// which is read as a word.
function count(text: string) {
  const cjk = text.match(/[぀-ヿ㐀-䶿一-鿿가-힯]/g)?.length ?? 0;
  const words = text.replace(/[぀-ヿ㐀-䶿一-鿿가-힯]/g, " ").match(/[\p{L}\p{N}][\p{L}\p{N}'’_-]*/gu)?.length ?? 0;
  const total = words + cjk;
  const minutes = Math.max(1, Math.round(total / (settings.get<number>("wordsPerMinute") || 230)));
  return { total, minutes };
}

async function show(doc: Document | null | undefined) {
  if (!doc || !languages.has(doc.language)) {
    await status.hide();
    return;
  }
  const { total, minutes } = count(await doc.text());
  await status.set({ text: `${total} words`, tooltip: `About ${minutes} min to read`, command: "word-count.show" });
}

events.on("editor.focus", (e) => show(e.document));
events.on("editor.change", (e) => show(e.document));
events.on("editor.open", (e) => show(e.document));

commands.register("word-count.show", async () => {
  const ed = await editor.active();
  if (!ed) return;
  const { total, minutes } = count(await ed.document.text());
  await window.notify(`${ed.document.name}: ${total} words, about ${minutes} min to read`);
});

editor.active().then((ed) => show(ed?.document));
