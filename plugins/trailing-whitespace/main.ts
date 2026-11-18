import { commands, editor, events, languages, settings, window, type Document, type Editor, type TextEdit } from "mayoi";

// trimEdits edits that remove the spaces and tabs at the ends of lines:
// none when the text has no trailing whitespace.
function trimEdits(text: string): TextEdit[] {
  const edits: TextEdit[] = [];
  text.split("\n").forEach((line, i) => {
    // The \r ending a CRLF line is the line's end, not trailing whitespace.
    const end = line.endsWith("\r") ? line.length - 1 : line.length;
    let start = end;
    while (start > 0 && (line[start - 1] === " " || line[start - 1] === "\t")) start--;
    if (start < end) {
      edits.push({
        range: { start: { line: i, character: start }, end: { line: i, character: end } },
        text: "",
      });
    }
  });
  return edits;
}

// trim trims the document of an editor, false when it has none to trim.
async function trim(ed: Editor): Promise<boolean> {
  const version = ed.document.version;
  const edits = trimEdits(await ed.document.text());
  if (edits.length === 0) return false;
  return ed.edit(edits, { version });
}

// formatsOnSave reports whether Mayoi formats the document before saving
// it, when this plugin's formatter trims the file as it is written.
function formatsOnSave(doc: Document): boolean {
  const on = settings.mayoi<Record<string, boolean>>("formatOnSave") ?? {};
  return Boolean(on["*"] || on[doc.language]);
}

languages.registerFormatter("trailing-whitespace.format", async (doc) => trimEdits(await doc.text()));

// A save without Format on Save leaves the trimming to this: the edit, then
// the save that writes it. The save cannot format, so it cannot loop.
events.on("editor.didSave", async (e) => {
  if (formatsOnSave(e.document)) return;
  const ed = (await editor.visible()).find((x) => x.document.id === e.document.id);
  if (!ed || !(await trim(ed))) return;
  await e.document.save();
});

commands.register("trailing-whitespace.trim", async () => {
  const ed = await editor.active();
  if (!ed) return;
  const edits = trimEdits(await ed.document.text());
  if (edits.length === 0) {
    await window.notify("No trailing whitespace");
    return;
  }
  await ed.edit(edits);
});
