// The types of the "mayoi" module, which Mayoi gives every plugin.
// See docs/PLUGINS.md in Mayoi's repository. The API is not stable yet.
declare module "mayoi" {
  export interface Disposable {
    dispose(): void;
  }

  /** A place in a document: a line and a column in UTF-16 code units, from 0. */
  export interface Position {
    line: number;
    character: number;
  }
  export interface Range {
    start: Position;
    end: Position;
  }
  /** A selection: where it started, and the caret. */
  export interface Selection {
    anchor: Position;
    active: Position;
  }
  export interface TextEdit {
    range: Range;
    text: string;
  }

  export interface Document {
    readonly id: number;
    /** The file's path, "" for an untitled document. On an SSH host, /ssh:host/path. */
    readonly path: string;
    readonly name: string;
    /** The language, as Mayoi's highlighter names it: "go", "typescript", "markdown". */
    readonly language: string;
    /** The version of the text, which every edit changes. */
    readonly version: number;
    readonly isDirty: boolean;
    readonly isUntitled: boolean;
    readonly lineCount: number;
    text(range?: Range): Promise<string>;
    lineAt(line: number): Promise<string>;
    save(): Promise<void>;
  }

  export interface Editor {
    readonly id: number;
    readonly document: Document;
    readonly selections: Selection[];
    readonly selection: Selection;
    /**
     * Makes edits as one step of undo. They are refused when the document changed since
     * version, the document's version as the editor was read by default.
     */
    edit(edits: TextEdit[], opts?: { version?: number; select?: Selection[] }): Promise<boolean>;
    /** Replaces each selection with text. */
    insert(text: string): Promise<boolean>;
    setSelections(selections: Selection[]): Promise<void>;
    reveal(line: number): Promise<void>;
  }

  export const version: string;
  export const platform: "darwin" | "linux" | "windows" | string;

  export const log: {
    info(...args: unknown[]): void;
    warn(...args: unknown[]): void;
    error(...args: unknown[]): void;
    debug(...args: unknown[]): void;
  };

  export const commands: {
    /** Runs fn for a command: one of plugin.json's, or a new one titled opts.title. */
    register(id: string, fn: (arg?: string) => unknown, opts?: { title?: string }): Disposable;
    /** Runs any command, Mayoi's too, with the argument `mayoi run id=arg` would give. */
    run(id: string, arg?: string): Promise<unknown>;
    list(): Promise<{ id: string; title: string }[]>;
  };

  export const editor: {
    /** The editor of the window in front, undefined for none. */
    active(): Promise<Editor | null>;
    visible(): Promise<Editor[]>;
  };

  export interface FileStat {
    isDir: boolean;
    size: number;
    mtime: number;
  }

  /** The folder of the window in front. Paths are absolute, or relative to the folder. */
  export const workspace: {
    root(): Promise<string>;
    isRemote(): Promise<boolean>;
    open(path: string, opts?: { line?: number; column?: number }): Promise<void>;
    readFile(path: string): Promise<string>;
    writeFile(path: string, text: string): Promise<void>;
    stat(path: string): Promise<FileStat | null>;
    findFiles(glob: string, opts?: { limit?: number }): Promise<string[]>;
    documents(): Promise<Document[]>;
  };

  export interface PickItem {
    label: string;
    description?: string;
    detail?: string;
    icon?: string;
  }

  export const window: {
    /** A toast; with an action, it resolves true once the action is chosen. */
    notify(message: string, opts?: { action?: string }): Promise<boolean>;
    fail(message: string): Promise<void>;
    pick<T extends PickItem | string>(items: T[], opts?: { placeholder?: string }): Promise<T | undefined>;
    prompt(opts?: { title?: string; value?: string; placeholder?: string }): Promise<string | undefined>;
    /** Shows a spinner in the status bar while fn runs. */
    progress<T>(title: string, fn: () => Promise<T> | T): Promise<T>;
    /** Opens text in a new untitled tab. */
    openText(text: string, opts?: { title?: string; language?: string }): Promise<void>;
  };

  export const settings: {
    /** A setting of the plugin: settings.json's plugins.<name>.<key>, or its default. */
    get<T = unknown>(key: string): T;
    all(): Record<string, unknown>;
    /** A setting of Mayoi's own, by its key in settings.json. */
    mayoi<T = unknown>(key: string): T;
    onChange(fn: () => void): Disposable;
  };

  /** A JSON store of the plugin's, global or of the folder in front. */
  export const storage: {
    get<T = unknown>(key: string, opts?: { folder?: boolean }): T | undefined;
    set(key: string, value: unknown, opts?: { folder?: boolean }): void;
    keys(opts?: { folder?: boolean }): string[];
  };

  export interface DocumentEvent {
    path: string;
    language: string;
    version: number;
    document: Document;
  }
  export interface EventMap {
    "editor.open": DocumentEvent;
    "editor.change": DocumentEvent;
    "editor.focus": DocumentEvent;
    "editor.didSave": DocumentEvent;
    "editor.close": { path: string };
    "workspace.changed": { root: string };
    "git.changed": { root: string };
    "theme.changed": { theme: string; dark: boolean };
    "settings.changed": Record<string, never>;
  }

  export const events: {
    on<K extends keyof EventMap>(name: K, fn: (e: EventMap[K]) => unknown): Disposable;
  };

  /** Programs, run on the folder's machine: here, or its SSH host. */
  export const process: {
    run(
      command: string,
      args?: string[],
      opts?: { cwd?: string; input?: string; timeout?: number }
    ): Promise<{ code: number; stdout: string; stderr: string }>;
  };

  export interface Diagnostic {
    range: Range;
    severity?: "error" | "warning" | "info" | "hint";
    message: string;
    code?: string;
  }

  export const languages: {
    /** Replaces the plugin's diagnostics of a file. */
    setDiagnostics(path: string, items: Diagnostic[], opts?: { source?: string }): Promise<void>;
    /** Clears the plugin's diagnostics of a file, or of every file. */
    clearDiagnostics(path?: string): Promise<void>;
    /**
     * Formats documents for a formatter of plugin.json: returns the whole new text, or edits,
     * or null to leave it as it is.
     */
    registerFormatter(
      id: string,
      fn: (doc: Document, opts: { tabSize: number; insertSpaces: boolean }) => Promise<string | TextEdit[] | null> | string | TextEdit[] | null
    ): Disposable;
    /** Answers hovers in a language with Markdown. */
    registerHover(language: string, fn: (doc: Document, pos: Position) => Promise<string | null> | string | null): Disposable;
  };

  export interface StatusItem {
    text: string;
    tooltip?: string;
    icon?: string;
    /** A command to run, or a function, on a click. */
    command?: string;
    onClick?: () => unknown;
  }

  /** An item of the status bar plugin.json declares. */
  export function statusItem(id: string): {
    set(item: StatusItem): Promise<void>;
    hide(): Promise<void>;
  };

  /** A node of a view, which Mayoi draws in its theme. */
  export type ViewNode =
    | { type: "stack" | "row"; children: ViewNode[]; gap?: number; padding?: number }
    | { type: "text"; text: string; muted?: boolean; bold?: boolean; size?: number }
    | { type: "markdown"; text: string }
    | { type: "code"; text: string; language?: string }
    | { type: "svg"; svg: string; height?: number }
    | { type: "list"; items: ViewListItem[]; empty?: string }
    | { type: "tree"; items: ViewTreeItem[] }
    | { type: "input"; value?: string; placeholder?: string; onChange?: (v: string) => unknown; onSubmit?: (v: string) => unknown }
    | { type: "select"; value?: string; options: string[]; onChange?: (v: string) => unknown }
    | { type: "checkbox"; label: string; checked?: boolean; onChange?: (v: boolean) => unknown }
    | { type: "button"; label: string; icon?: string; primary?: boolean; onClick?: () => unknown }
    | { type: "progress"; value?: number }
    | { type: "divider" }
    | { type: "empty"; text: string; icon?: string };
  export interface ViewAction {
    icon: string;
    tooltip: string;
    onClick: () => unknown;
  }
  export interface ViewListItem {
    key?: string;
    label: string;
    description?: string;
    icon?: string;
    onClick?: () => unknown;
    actions?: ViewAction[];
  }
  export interface ViewTreeItem extends ViewListItem {
    children?: ViewTreeItem[];
    open?: boolean;
  }

  export const views: {
    /** Draws a view plugin.json declares, until it renders again. */
    render(id: string, tree: ViewNode): Promise<void>;
    /** Runs fn as the view shows, to render it. */
    onShow(id: string, fn: () => unknown): Disposable;
    reveal(id: string): Promise<void>;
  };

  export const previews: {
    /** Renders a preview plugin.json declares: SVG or Markdown text, or a view. */
    register(id: string, fn: (text: string, ctx: { path: string }) => Promise<string | ViewNode> | string | ViewNode): Disposable;
    /** Renders the preview again, as when what it shows changed. */
    refresh(id: string): Promise<void>;
  };

  export interface Webview {
    post(message: unknown): Promise<void>;
    onMessage(fn: (message: unknown) => unknown): Disposable;
    close(): Promise<void>;
  }

  export const webview: {
    /** Opens a webview plugin.json declares; its page calls mayoi.post(message) and listens with mayoi.onMessage. */
    open(id: string, opts?: { title?: string }): Promise<Webview>;
  };

  /** A session of an agent plugin.json declares, as the Sessions view lists it. */
  export interface AgentSession {
    /** What the resume command's {id} is; a session without one is left out. */
    id: string;
    title?: string;
    /** The folder it ran in, which it resumes in. */
    folder?: string;
    /** When it was used last: milliseconds since 1970, or ISO 8601 text. */
    updated?: number | string;
    /** The first thing asked in it. */
    prompt?: string;
  }

  export const agents: {
    /**
     * Lists the sessions of an agent plugin.json declares without `sessions`. Mayoi calls fn
     * off its main thread as the Sessions view reads, waits 1.5 s for it, and gives up after 10 s.
     */
    register(id: string, fn: () => Promise<AgentSession[]> | AgentSession[]): Disposable;
  };

  export const ai: {
    /** Completes a prompt with Mon's AI gateway; the user must be signed in. */
    complete(prompt: string, opts?: { system?: string }): Promise<string>;
  };

  const mayoi: {
    version: typeof version;
    platform: typeof platform;
    log: typeof log;
    commands: typeof commands;
    editor: typeof editor;
    workspace: typeof workspace;
    window: typeof window;
    settings: typeof settings;
    storage: typeof storage;
    events: typeof events;
    process: typeof process;
    languages: typeof languages;
    statusItem: typeof statusItem;
    views: typeof views;
    previews: typeof previews;
    webview: typeof webview;
    agents: typeof agents;
    ai: typeof ai;
  };
  export default mayoi;
}

declare function setTimeout(fn: (...args: unknown[]) => void, ms?: number, ...args: unknown[]): unknown;
declare function clearTimeout(t: unknown): void;
declare function setInterval(fn: (...args: unknown[]) => void, ms?: number, ...args: unknown[]): unknown;
declare function clearInterval(t: unknown): void;
declare const console: {
  log(...args: unknown[]): void;
  info(...args: unknown[]): void;
  warn(...args: unknown[]): void;
  error(...args: unknown[]): void;
  debug(...args: unknown[]): void;
};
