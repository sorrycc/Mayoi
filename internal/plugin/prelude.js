// The mayoi module, as every plugin's runtime has it before the plugin's
// own code runs. It is built on the host's few functions, __mayoiHost:
// call (async, to the app), callSync (to the registry), reply, bind and
// log. Values cross as JSON text.
(function () {
  "use strict";
  const host = globalThis.__mayoiHost;
  delete globalThis.__mayoiHost;
  const enc = (v) => (v === undefined ? "null" : JSON.stringify(v));
  const call = (method, args) => host.call(method, enc(args ?? {}));
  const callSync = (method, args) => host.callSync(method, enc(args ?? {}));

  const fmt = (args) =>
    args
      .map((a) => {
        if (typeof a === "string") return a;
        if (a instanceof Error) return a.stack || String(a);
        try {
          return JSON.stringify(a);
        } catch {
          return String(a);
        }
      })
      .join(" ");
  const log = {
    info: (...a) => host.log("info", fmt(a)),
    warn: (...a) => host.log("warn", fmt(a)),
    error: (...a) => host.log("error", fmt(a)),
    debug: (...a) => host.log("debug", fmt(a)),
  };
  globalThis.console = { log: log.info, info: log.info, warn: log.warn, error: log.error, debug: log.debug };

  // Handlers the host calls back by id: of commands, events, views, items.
  const commandFns = new Map();
  const listeners = new Map();
  const formatters = new Map();
  const previewFns = new Map();
  const hoverFns = new Map();
  const viewShows = new Map();
  const webviewFns = new Map();
  const agentFns = new Map();
  let handlerSeq = 0;
  const handlers = new Map(); // id -> fn
  const owned = new Map(); // owner -> [ids], dropped as the owner renders anew

  function own(owner, fn) {
    const id = "h" + ++handlerSeq;
    handlers.set(id, fn);
    let ids = owned.get(owner);
    if (!ids) owned.set(owner, (ids = []));
    ids.push(id);
    return { $h: id };
  }
  function drop(owner) {
    for (const id of owned.get(owner) || []) handlers.delete(id);
    owned.delete(owner);
  }
  // withHandlers replaces the functions of a tree with ids the host sends
  // back: { onClick: fn } becomes { onClick: { $h: "h1" } }.
  function withHandlers(owner, v) {
    if (typeof v === "function") return own(owner, v);
    if (Array.isArray(v)) return v.map((x) => withHandlers(owner, x));
    if (v && typeof v === "object") {
      const out = {};
      for (const k of Object.keys(v)) out[k] = withHandlers(owner, v[k]);
      return out;
    }
    return v;
  }
  const disposable = (fn) => ({ dispose: fn });

  // Documents and editors, as the host describes them.
  function doc(d) {
    if (!d) return null;
    return {
      id: d.id,
      path: d.path,
      name: d.name,
      language: d.language,
      version: d.version,
      isDirty: d.dirty,
      isUntitled: d.untitled,
      lineCount: d.lineCount,
      text: (range) => call("document.text", { doc: d.id, range }),
      lineAt: (line) => call("document.line", { doc: d.id, line }),
      save: () => call("document.save", { doc: d.id }),
    };
  }
  function editorOf(e) {
    if (!e) return null;
    return {
      id: e.id,
      document: doc(e.document),
      selections: e.selections,
      get selection() {
        return e.selections[0];
      },
      // edit applies edits, [{ range, text }], made against version, the
      // document's version they were computed from: the document's own
      // when not given. Edits of a document changed since are refused.
      edit: (edits, opts) =>
        call("editor.edit", { editor: e.id, edits, version: opts?.version ?? e.document.version, select: opts?.select }),
      insert: (text) => call("editor.insert", { editor: e.id, text }),
      setSelections: (selections) => call("editor.select", { editor: e.id, selections }),
      reveal: (line) => call("editor.reveal", { editor: e.id, line }),
    };
  }

  const mayoi = {
    version: callSync("app.version"),
    platform: callSync("app.platform"),
    log,
    commands: {
      register(id, fn, opts) {
        commandFns.set(id, fn);
        callSync("commands.register", { id, title: opts?.title });
        return disposable(() => {
          commandFns.delete(id);
          callSync("commands.unregister", { id });
        });
      },
      run: (id, arg) => call("commands.run", { id, arg }),
      list: () => call("commands.list"),
    },
    editor: {
      active: async () => editorOf(await call("editor.active")),
      visible: async () => (await call("editor.visible")).map(editorOf),
    },
    workspace: {
      root: () => call("workspace.root"),
      isRemote: () => call("workspace.isRemote"),
      open: (path, opts) => call("workspace.open", { path, line: opts?.line, column: opts?.column }),
      readFile: (path) => call("workspace.readFile", { path }),
      writeFile: (path, text) => call("workspace.writeFile", { path, text }),
      stat: (path) => call("workspace.stat", { path }),
      findFiles: (glob, opts) => call("workspace.findFiles", { glob, limit: opts?.limit }),
      documents: async () => (await call("workspace.documents")).map(doc),
    },
    window: {
      notify: (message, opts) =>
        call("window.notify", { message, action: opts?.action, kind: "info" }).then((chosen) => chosen),
      fail: (message) => call("window.notify", { message, kind: "error" }),
      async pick(items, opts) {
        const list = items.map((it) => (typeof it === "string" ? { label: it } : it));
        const i = await call("window.pick", {
          items: list.map(({ label, description, detail, icon }) => ({ label, description, detail, icon })),
          placeholder: opts?.placeholder,
        });
        return i == null ? undefined : items[i];
      },
      prompt: (opts) =>
        call("window.prompt", { title: opts?.title, value: opts?.value, placeholder: opts?.placeholder }).then((v) =>
          v == null ? undefined : v
        ),
      progress: (title, fn) => {
        call("window.progress", { title, done: false });
        return Promise.resolve()
          .then(fn)
          .finally(() => call("window.progress", { title, done: true }));
      },
      openText: (text, opts) => call("window.openText", { text, title: opts?.title, language: opts?.language }),
    },
    settings: {
      get: (key) => callSync("settings.get", { key }),
      all: () => callSync("settings.all"),
      mayoi: (key) => callSync("settings.mayoi", { key }),
      onChange: (fn) => mayoi.events.on("settings.changed", fn),
    },
    storage: {
      get: (key, opts) => callSync("storage.get", { key, folder: !!opts?.folder }),
      set: (key, value, opts) => callSync("storage.set", { key, value, folder: !!opts?.folder }),
      keys: (opts) => callSync("storage.keys", { folder: !!opts?.folder }),
    },
    events: {
      on(name, fn) {
        let set = listeners.get(name);
        if (!set) {
          listeners.set(name, (set = new Set()));
          callSync("events.subscribe", { name });
        }
        set.add(fn);
        return disposable(() => set.delete(fn));
      },
    },
    process: {
      run: (command, args, opts) =>
        call("process.run", { command, args: args ?? [], cwd: opts?.cwd, input: opts?.input, timeout: opts?.timeout }),
    },
    languages: {
      setDiagnostics: (path, items, opts) => call("languages.diagnostics", { path, items, source: opts?.source }),
      clearDiagnostics: (path) => call("languages.diagnostics", { path, items: [], clear: path === undefined }),
      registerFormatter(id, fn) {
        formatters.set(id, fn);
        return disposable(() => formatters.delete(id));
      },
      registerHover(language, fn) {
        hoverFns.set(language, fn);
        callSync("languages.hover", { language });
        return disposable(() => hoverFns.delete(language));
      },
    },
    statusItem(id) {
      return {
        set(item) {
          drop("status:" + id);
          return call("status.set", { id, item: item && withHandlers("status:" + id, item) });
        },
        hide() {
          drop("status:" + id);
          return call("status.set", { id, item: null });
        },
      };
    },
    views: {
      render(id, tree) {
        drop("view:" + id);
        return call("views.render", { id, tree: withHandlers("view:" + id, tree) });
      },
      onShow(id, fn) {
        viewShows.set(id, fn);
        return disposable(() => viewShows.delete(id));
      },
      reveal: (id) => call("views.reveal", { id }),
    },
    previews: {
      register(id, fn) {
        previewFns.set(id, fn);
        return disposable(() => previewFns.delete(id));
      },
      refresh: (id) => call("previews.refresh", { id }),
    },
    webview: {
      async open(id, opts) {
        await call("webview.open", { id, title: opts?.title });
        return {
          post: (message) => call("webview.post", { id, message }),
          onMessage(fn) {
            let set = webviewFns.get(id);
            if (!set) webviewFns.set(id, (set = new Set()));
            set.add(fn);
            return disposable(() => set.delete(fn));
          },
          close: () => call("webview.close", { id }),
        };
      },
    },
    agents: {
      register(id, fn) {
        agentFns.set(id, fn);
        return disposable(() => agentFns.delete(id));
      },
    },
    ai: {
      complete: (prompt, opts) => call("ai.complete", { prompt, system: opts?.system }),
    },
  };
  Object.freeze(mayoi);
  globalThis.__mayoi = mayoi;

  // dispatch is how the host calls the plugin: with an id to reply to, a
  // kind, and its payload as JSON text.
  async function run(kind, p) {
    switch (kind) {
      case "command": {
        const fn = commandFns.get(p.id);
        if (!fn) throw new Error(`command ${p.id} is not registered`);
        return await fn(p.arg);
      }
      case "event": {
        const data = p.data;
        if (data && typeof data === "object" && data.document) data.document = doc(data.document);
        for (const fn of listeners.get(p.name) || []) {
          try {
            await fn(data);
          } catch (e) {
            log.error(`${p.name} listener:`, e);
          }
        }
        return null;
      }
      case "handler": {
        const fn = handlers.get(p.id);
        if (fn) await fn(p.value);
        return null;
      }
      case "format": {
        const fn = formatters.get(p.id);
        if (!fn) throw new Error(`formatter ${p.id} is not registered`);
        return (await fn(doc(p.document), p.options || {})) ?? null;
      }
      case "preview": {
        const fn = previewFns.get(p.id);
        if (!fn) throw new Error(`preview ${p.id} is not registered`);
        const out = await fn(p.text, { path: p.path });
        if (out && typeof out === "object") {
          drop("preview:" + p.id);
          return withHandlers("preview:" + p.id, out);
        }
        return out ?? "";
      }
      case "hover": {
        const fn = hoverFns.get(p.language);
        return fn ? ((await fn(doc(p.document), p.position)) ?? null) : null;
      }
      case "viewShow": {
        const fn = viewShows.get(p.id);
        if (fn) await fn();
        return null;
      }
      case "webview": {
        for (const fn of webviewFns.get(p.id) || []) await fn(p.message);
        return null;
      }
      case "agentSessions": {
        const fn = agentFns.get(p.id);
        if (!fn) throw new Error(`agent ${p.id} has no sessions function: call agents.register("${p.id}", fn)`);
        const out = await fn();
        if (!Array.isArray(out)) throw new Error(`the sessions of agent ${p.id} are not an array`);
        return out;
      }
      case "call": {
        // An export of the entry module, as a user script's commands.
        const fn = globalThis.__mayoiExports?.[p.name];
        if (typeof fn !== "function") throw new Error(`${p.name} is not an exported function`);
        return (await fn(p.arg)) ?? null;
      }
    }
    throw new Error(`unknown call ${kind}`);
  }
  host.bind((id, kind, payload) => {
    let p;
    try {
      p = JSON.parse(payload);
    } catch (e) {
      host.reply(id, false, String(e));
      return;
    }
    run(kind, p).then(
      (v) => {
        let out;
        try {
          out = enc(v);
        } catch (e) {
          host.reply(id, false, "result is not JSON: " + e);
          return;
        }
        host.reply(id, true, out);
      },
      (e) => host.reply(id, false, e instanceof Error ? e.stack || e.message : String(e))
    );
  });
})();
