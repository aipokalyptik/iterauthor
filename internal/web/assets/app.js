"use strict";
const $ = (q, root = document) => root.querySelector(q);
const $$ = (q, root = document) => [...root.querySelectorAll(q)];
const esc = (v) =>
  String(v ?? "").replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
const roles = {
  knowledge: "Knowledge selection",
  "outline-context": "Outline selection",
  outline: "Outlining",
  prose: "Prose writing",
  consistency: "Consistency review",
  style: "Style review",
};
const needsTools = (r) =>
  ["knowledge", "outline-context", "consistency"].includes(r);
const ui = {
  view: null,
  section: "outline",
  selected: "",
  field: "outline",
  original: "",
  dirty: false,
  documentKey: "",
  load: 0,
  conversation: null,
  assistant: false,
  draft: "",
  refreshPromise: null,
  wizard: null,
  dialogDirty: false,
  settingsDirty: false,
  refreshAgain: false,
  sending: false,
  savedDraft: "",
  conversationLoad: 0,
  sendError: "",
  sendingSince: "",
  connectionLost: false,
  dismissedWork: "",
};
let noticeTimer, draftTimer;
let pendingDraft = Promise.resolve();

async function api(path, body) {
  const response = await fetch(
    path,
    body === undefined
      ? {}
      : {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Iterauthor": "1" },
          body: JSON.stringify(body),
        },
  );
  const data = await response.json();
  if (!response.ok)
    throw new Error(data.error || `Request failed (${response.status})`);
  return data;
}
async function command(action, values = {}) {
  const result = await api("/api/command", { action, ...values });
  await refresh();
  return result;
}
function notice(text, error = false) {
  if (error && $("#dialog").open) {
    let alert = $("#dialog-notice");
    if (!alert) {
      alert = document.createElement("p");
      alert.id = "dialog-notice";
      alert.className = "error-box";
      alert.setAttribute("role", "alert");
      $("#dialog-body").prepend(alert);
    }
    alert.textContent = text;
  }
  clearTimeout(noticeTimer);
  const n = $("#notice");
  n.textContent = text;
  n.className = error ? "error" : "";
  n.hidden = false;
  noticeTimer = setTimeout(() => (n.hidden = true), error ? 11000 : 5000);
}
function report(e) {
  notice(e.message || String(e), true);
}
function title(id) {
  return (
    ui.view?.config.nodes[id]?.title ||
    ui.view?.config.knowledge[id]?.title ||
    id
  );
}
function node() {
  return ui.view.config.nodes[ui.selected];
}
function leaves(id = ui.view.config.root) {
  const n = ui.view.config.nodes[id];
  return n ? (n.children?.length ? n.children.flatMap(leaves) : [id]) : [];
}
function ancestors(id) {
  const a = [];
  let n = ui.view.config.nodes[id];
  while (n) {
    a.unshift(n);
    n = ui.view.config.nodes[n.parent];
  }
  return a;
}
function resolvedModel(id, role) {
  for (const n of ancestors(id).reverse())
    if (n.models?.[role]) return [n.models[role], n.title];
  const c = ui.view.config;
  return [
    c.feature_models?.[role] || c.base_model,
    c.feature_models?.[role] ? "Feature default" : "Base model",
  ];
}
function badge(text) {
  const type = /Failed|Invalidated|Canceled/.test(text)
    ? "error"
    : /review|findings|Retained/.test(text)
      ? "warn"
      : "";
  return `<span class="badge ${type}">${esc(text)}</span>`;
}
function configured() {
  const c = ui.view.config;
  return ui.view.demo || Boolean(c.models[c.base_model]?.model);
}
function modelOptions(selected = "", inherit = "", tools = false) {
  const c = ui.view.config, groups = new Map();
  let html = inherit ? `<option value="">${esc(inherit)}</option>` : "";
  for (const [id,m] of Object.entries(c.models)) {
    if (!(m.model || ui.view.demo) || (tools && (!m.tools || m.tools_unverified) && id !== selected)) continue;
    const group = m.connection || "";
    if (!groups.has(group)) groups.set(group, []);
    groups.get(group).push(`<option value="${esc(id)}" ${id === selected ? "selected" : ""} ${tools && (!m.tools || m.tools_unverified) ? "disabled" : ""}>${esc(m.name || m.model)}${modelAvailability(m)}</option>`);
  }
  for (const [group, options] of groups) html += `<optgroup label="${esc(c.connections?.[group]?.name || "Saved models")}">${options.join("")}</optgroup>`;
  return html;
}
function requireSaved() {
  if (ui.dirty || ui.settingsDirty) {
    notice(
      "Save or discard your current text before starting another operation.",
      true,
    );
    return false;
  }
  return true;
}
async function mayNavigate() {
  if (!ui.dirty && !ui.settingsDirty) return true;
  return await confirmChoice(
    "Unsaved text",
    "Save your changes before leaving this document?",
    [
      { label: "Save and continue", value: "save", primary: true },
      { label: "Discard changes", value: "discard" },
      { label: "Keep editing", value: "cancel" },
    ],
  ).then(async (answer) => {
    if (answer === "save") {
      if (ui.settingsDirty) await saveSettings();
      else await saveDocument();
      return true;
    }
    if (answer === "discard") {
      ui.dirty = false;
      ui.settingsDirty = false;
      return true;
    }
    return false;
  });
}
async function navigate(section, id = "") {
  if (!(await mayNavigate())) return;
  ui.section = section;
  ui.documentKey = "";
  ui.load++;
  if (section === "outline") {
    ui.selected = id || (node() ? ui.selected : ui.view.config.root);
    ui.field = "outline";
  }
  if (section === "knowledge") {
    ui.selected = id || Object.keys(ui.view.config.knowledge)[0] || "";
    ui.field = "entry";
  }
  renderShell();
  await renderMain();
}
async function refresh() {
  if (ui.refreshPromise) {
    ui.refreshAgain = true;
    return ui.refreshPromise;
  }
  ui.refreshPromise = (async () => {
    do {
      ui.refreshAgain = false;
      const old = ui.view;
      ui.view = await api("/api/state");
      if (!ui.selected) ui.selected = ui.view.config.root;
      if (!old) {
        renderShell();
        await renderMain();
        continue;
      }
      renderShell();
      const finished = old.busy && !ui.view.busy;
      if (ui.conversation && (finished || old.lastRun !== ui.view.lastRun)) {
        await loadConversation(ui.conversation.id);
      }
      if (
        ui.section === "activity" &&
        (finished || old.lastRun !== ui.view.lastRun)
      )
        await renderMain();
      if (ui.section === "models" && JSON.stringify(old.catalogs) !== JSON.stringify(ui.view.catalogs)) {
        for (const [id,state] of Object.entries(ui.view.catalogs || {})) {
          const status = document.getElementById("catalog-status-"+id);
          if (status) status.textContent = catalogStatus(state);
          const button = $("[data-refresh-connection='"+id+"']");
          if (button) button.disabled = state.refreshing;
        }
        if (!$("#main").contains(document.activeElement)) {
          const selections = $$("#main select").map(el => [el.id,el.value]);
          renderModels();
          for (const [id,value] of selections) { const el = document.getElementById(id); if (el && Array.from(el.options).some(o=>o.value===value)) el.value=value; }
        }
      }
      const editor = $("#source-editor");
      if (editor) {
        editor.disabled = ui.view.busy;
        updateEditor();
      }
      const send = $("#chat-form button[type=submit]");
      if (send) send.disabled = ui.view.busy || ui.sending;
      if (
        !ui.dirty &&
        ui.documentKey &&
        (finished || old.lastRun !== ui.view.lastRun) &&
        ["outline", "knowledge"].includes(ui.section)
      )
        await renderDocument();
    } while (ui.refreshAgain);
  })()
    .catch(report)
    .finally(() => (ui.refreshPromise = null));
  return ui.refreshPromise;
}

function renderShell() {
  const v = ui.view,
    c = v.config;
  document.title = `${c.title} · Iterauthor`;
  $("#project-title").textContent = c.title;
  $("#navigation").innerHTML = [
    ["outline", "Outline", "☷"],
    ["knowledge", "World wiki", "◇"],
    ["manuscript", "Manuscript", "▤"],
    ["activity", "Activity", "◷"],
    ["models", "Models", "◉"],
  ]
    .map(
      ([id, label, icon]) =>
        `<button class="nav-item ${ui.section === id ? "active" : ""}" data-nav="${id}" ${ui.section === id ? 'aria-current="page"' : ""}><span class="nav-icon" aria-hidden="true">${icon}</span>${label}${id === "outline" ? `<span class="nav-count">${leaves().length} passage${leaves().length === 1 ? "" : "s"}</span>` : ""}</button>`,
    )
    .join("");
  $("#breadcrumb").textContent =
    ({
      outline: "Outline",
      knowledge: "World wiki",
      models: "Model connections",
      activity: "Generation history",
      manuscript: "Working manuscript",
      settings: "Project settings",
    }[ui.section] || "") +
    (["outline", "knowledge"].includes(ui.section)
      ? ` / ${title(ui.selected)}`
      : "");
  $("#mode").textContent = v.demo
    ? "Demo mode"
    : v.busy
      ? "Working"
      : ui.dirty
        ? "Unsaved text"
        : "Workspace";
  $("#mode").className = "badge " + (v.demo ? "warn" : "neutral");
  let finish = $("#finish-editing");
  if (!finish) {
    finish = document.createElement("button");
    finish.id = "finish-editing";
    finish.dataset.action = "finish-editing";
    finish.dataset.write = "";
    finish.textContent = "Finish editing";
    $("#mode").after(finish);
  }
  finish.hidden = !c.generate_after_editing || !v.editing;
  renderWorkStatus();
  renderAssistantStatus();
  const changes = v.state.changes || [];
  $("#change-banner").hidden = !changes.length;
  $("#change-banner").innerHTML =
    `<span>${changes.length} saved change${changes.length === 1 ? "" : "s"} · Decide which prose to revisit before drafting.</span><button data-action="changes" ${v.busy ? "disabled" : ""}>Review changes</button>`;
  let tree = "";
  if (ui.section === "outline" || ui.section === "manuscript") {
    const walk = (id, depth) => {
      const n = c.nodes[id];
      tree += `<button class="tree-item ${ui.selected === id ? "active" : ""}" data-item="${esc(id)}" data-section="outline" style="--depth:${depth}" title="${esc(n.title)}"><span class="dot ${v.statuses[id] === "Available" ? "ready" : ""}">${n.children?.length ? "▾" : "●"}</span><span class="tree-title">${esc(n.title)}</span></button>`;
      (n.children || []).forEach((child) => walk(child, depth + 1));
    };
    walk(c.root, 0);
    $("#tree-heading").innerHTML =
      `STORY STRUCTURE <button data-action="add-child" title="Add outline below selected item" ${v.busy ? "disabled" : ""}>+ Add</button>`;
  } else if (ui.section === "knowledge") {
    for (const e of Object.values(c.knowledge).sort((a, b) =>
      a.title.localeCompare(b.title),
    ))
      tree += `<button class="tree-item ${ui.selected === e.id ? "active" : ""}" data-item="${esc(e.id)}" data-section="knowledge"><span class="dot">◇</span><span class="tree-title">${esc(e.title)}</span></button>`;
    $("#tree-heading").innerHTML =
      `WORLD ENTRIES <button data-action="add-entry" ${v.busy ? "disabled" : ""}>+ Add</button>`;
  } else $("#tree-heading").textContent = "";
  $("#tree").innerHTML = tree;
  $$("[data-write]").forEach((b) => (b.disabled = v.busy));
}

function elapsed(started) {
  const seconds = Math.max(0, Math.floor((Date.now() - Date.parse(started)) / 1000));
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}
function renderWorkStatus() {
  const v = ui.view, w = v?.work, box = $("#work-status");
  if (!v) return;
  box.hidden = !v.busy && (!w || ui.dismissedWork === w.started);
  if (box.hidden) return;
  box.classList.toggle("work-failed", !v.busy && ["Failed", "Needs review", "Canceled"].includes(w?.status));
  box.innerHTML = `<span>${esc(v.canceling ? "Stopping the current operation…" : v.progress || "Working…")}${v.busy && w?.started ? ` · <span data-elapsed="${esc(w.started)}">${elapsed(w.started)}</span> elapsed` : ""}${v.queue?.length ? ` · ${v.queue.length} queued` : ""}</span><div class="actions">${v.busy ? `<button data-action="cancel" ${v.canceling ? "disabled" : ""}>Stop operation</button>` : `${w?.run ? `<button data-run="${esc(w.run)}">Inspect result</button>` : ""}${w?.conversation === ui.conversation?.id && ["Failed", "Canceled"].includes(w?.status) ? '<button data-action="retry-message">Retry message</button>' : ""}<button data-action="dismiss-work">Dismiss</button>`}</div>`;
}
function renderAssistantStatus() {
  const box = $("#assistant-status");
  if (!box || !ui.view) return;
  const w = ui.view.work;
  const active = ui.view.busy && w?.conversation === ui.conversation?.id;
  const last = ui.conversation?.turns?.at(-1);
  const interrupted = last?.role === "author" && !active && !ui.sending && !ui.connectionLost;
  box.hidden = !ui.sending && !active && !ui.sendError && !ui.connectionLost && !interrupted;
  box.classList.toggle("error-box", Boolean(ui.sendError) || interrupted);
  box.textContent = ui.sendError || (ui.sending
    ? `Sending message… ${elapsed(ui.sendingSince)} elapsed`
    : ui.connectionLost
      ? "Connection interrupted. Reconnecting and checking the operation status…"
      : active
        ? `${ui.view.canceling ? "Stopping…" : ui.view.progress || "Working…"} · ${elapsed(w.started)} elapsed`
        : interrupted ? "No completed reply is recorded. Check Activity for an interrupted run before retrying." : "");
  const send = $("#chat-form button[type=submit]");
  if (send) {
    send.disabled = ui.view.busy || ui.sending;
    send.textContent = ui.sending ? "Sending…" : active ? "Working…" : "Send";
  }
}

async function renderMain() {
  const main = $("#main");
  ui.documentKey = "";
  if (["outline", "knowledge"].includes(ui.section)) {
    await renderDocument();
    return;
  }
  if (ui.section === "models") {
    renderModels();
    return;
  }
  if (ui.section === "settings") {
    renderSettings();
    return;
  }
  const section = ui.section,
    load = ++ui.load;
  if (section === "manuscript") {
    const data = await api("/api/manuscript");
    if (load !== ui.load) return;
    main.innerHTML = `<div class="page-head"><div class="row spread"><h2>Your manuscript</h2><button data-action="export">Export Markdown</button></div><p>Active passages, in story order. Missing or outdated text stays visibly marked.</p></div><div class="reading manuscript">${esc(data.text)}</div>`;
    return;
  }
  if (section === "activity") {
    const rows = await api("/api/runs");
    if (load !== ui.load) return;
    main.innerHTML = `<div class="page-head"><div class="row spread"><h2>Activity</h2><button data-action="refresh-main">Refresh</button></div><p>Inspect drafts, review findings, and the exact context behind each result.</p></div>${rows.length ? rows.map((r) => `<article class="card"><div class="row spread"><div><div class="eyebrow">${esc(r.kind.toUpperCase())} · ${esc(new Date(r.started).toLocaleString())}</div><h3>${esc(title(r.target))}</h3></div>${badge(ui.view.state.invalid_runs?.[r.id] ? "Invalidated" : r.finished ? r.status : ui.view.busy ? r.status : "Interrupted")}</div><p class="muted small">${r.calls} model calls · ${r.candidates} candidate${r.candidates === 1 ? "" : "s"}${r.edits ? ` · ${r.edits} proposed edits` : ""}</p>${r.error ? `<p class="small danger">${esc(r.error)}</p>` : ""}<button data-run="${esc(r.id)}">Inspect result</button></article>`).join("") : `<div class="empty"><h3>Your first draft starts with an outline</h3>Choose a passage and select Draft passage. Its drafts and reviews will appear here.<p><button data-nav="outline">Go to outline</button></p></div>`}`;
  }
}

function tabs() {
  return ui.section === "knowledge"
    ? [
        ["entry", "Entry"],
        ["notes", "Private notes"],
        ["history", "History"],
      ]
    : [
        ["outline", "Outline"],
        ["prose", "Prose"],
        ["style", "Style"],
        ["context", "Context & instructions"],
        ["notes", "Private notes"],
        ["history", "History"],
      ];
}
async function renderDocument() {
  const id = ui.selected,
    field = ui.field,
    load = ++ui.load,
    oldKey = ui.documentKey,
    n = node(),
    entry = ui.view.config.knowledge[id],
    main = $("#main");
  if (!n && !entry) {
    main.innerHTML = `<div class="empty"><h3>Build the world around your story</h3>Keep characters, places, and world details in linked entries.<p><button class="primary" data-action="add-entry">Create a world entry</button></p></div>`;
    return;
  }
  let data = { text: "" },
    effective = "";
  if (
    !["history", "context"].includes(field) &&
    !(field === "prose" && n?.children?.length)
  )
    data = await api(`/api/source/${encodeURIComponent(id)}/${field}`);
  if (field === "style")
    effective = (await api(`/api/context/${encodeURIComponent(id)}`)).style;
  if (load !== ui.load || (ui.dirty && oldKey === `${id}/${field}`)) return;
  const parent = n?.parent,
    titleText = title(id),
    leaf = n && !n.children?.length;
  main.innerHTML = `<div class="page-head"><div class="eyebrow">${entry ? esc(entry.kind) : parent ? esc(title(parent)) : "STORY OVERVIEW"}</div><div class="row spread"><h2>${esc(titleText)}</h2>${leaf ? badge(ui.view.statuses[id] || "Missing") : ""}</div><p>${entry ? "Facts and details you can attach to any outline." : leaf ? "Give this passage as much detail as it needs. Your outline guides the eventual prose." : `${leaves(id).length} passages in this branch. Add detail at any level.`}</p><div class="actions">${n ? `<button class="primary" data-action="generate" data-write>${leaf ? "Draft passage" : "Draft this branch"}</button><button data-action="expand" data-write>Develop outline</button>` : ""}<button data-action="discuss" data-write>Discuss this ${entry ? "entry" : "outline"}</button><button data-action="rename" data-write>Rename</button></div><div class="tabs" role="tablist">${tabs()
    .map(
      ([key, label]) =>
        `<button class="tab ${field === key ? "active" : ""}" data-tab="${key}" role="tab" aria-selected="${field === key}">${label}</button>`,
    )
    .join("")}</div></div><div id="document-content"></div>`;
  const content = $("#document-content");
  if (field === "context") {
    renderContext(content);
    return;
  }
  if (field === "history") {
    await renderHistory(content, id, load);
    return;
  }
  if (field === "prose" && !leaf) {
    const texts = await Promise.all(
      leaves(id).map(async (ref) => ({
        id: ref,
        text: (await api(`/api/source/${ref}/prose`)).text,
      })),
    );
    if (load !== ui.load) return;
    content.innerHTML = texts
      .map(
        (row) =>
          `<article class="card"><div class="row spread"><h3>${esc(title(row.id))}</h3><button data-item="${row.id}" data-section="outline">Open passage</button></div><div class="prose-preview">${esc(row.text || "No prose yet.")}</div></article>`,
      )
      .join("");
    return;
  }
  ui.original = data.text;
  ui.dirty = false;
  ui.documentKey = `${id}/${field}`;
  const label = tabs().find(([key]) => key === field)?.[1] || field;
  content.innerHTML = `${field === "notes" ? '<p class="hint small">These notes are for you. They are never sent to a model.</p>' : ""}${field === "style" ? `<p class="muted small">Local style ${n.replace_style ? "replaces" : "adds to"} the inherited guide. <button class="quiet small link" data-action="guidance">Change inheritance or model choices</button></p>` : ""}<section class="document"><div class="document-bar"><span>${esc(label)} · <span id="save-state">Saved</span></span><div class="actions"><button id="discard-text" data-action="discard" hidden>Discard changes</button><button id="save-text" class="primary" data-action="save" disabled>Save changes</button></div></div><textarea id="source-editor" aria-label="${esc(label)} text" class="${field === "prose" ? "prose" : ""}" spellcheck="true" ${ui.view.busy ? "disabled" : ""} placeholder="${field === "prose" ? "Draft this passage to create a candidate, or write your own prose here." : field === "notes" ? "Your private notes…" : "Write the details that matter to this part of the story…"}">${esc(data.text)}</textarea></section><div class="document-footer"><span id="word-count">${wordCount(data.text)} words</span><span>Save with ⌘S / Ctrl+S · Files remain editable outside Iterauthor</span></div>${field === "prose" ? `<div class="actions" style="margin-top:18px"><button data-action="generate" data-force="true" data-write>Generate a replacement</button>${ui.view.state.passages[id]?.candidate ? `<button data-run="${esc(ui.view.state.passages[id].candidate)}">Review latest candidate</button>` : ""}<button data-action="invalidate" data-write>Mark prose for regeneration</button></div>` : ""}${field === "style" ? `<details><summary>Effective style at this outline</summary><pre>${esc(effective)}</pre></details>` : ""}`;
  $("#source-editor").addEventListener("input", () => {
    ui.dirty = $("#source-editor").value !== ui.original;
    updateEditor();
  });
  renderShell();
  updateEditor();
}
function wordCount(text) {
  return text.trim() ? text.trim().split(/\s+/).length : 0;
}
function updateEditor() {
  const e = $("#source-editor");
  if (!e) return;
  $("#save-state").textContent = ui.dirty ? "Unsaved changes" : "Saved";
  $("#save-text").disabled = !ui.dirty || ui.view.busy;
  $("#discard-text").hidden = !ui.dirty;
  $("#word-count").textContent = `${wordCount(e.value)} words`;
}
async function saveDocument() {
  const editor = $("#source-editor");
  if (!editor || !ui.dirty) return;
  const value = editor.value;
  await command("save", {
    id: ui.selected,
    field: ui.field,
    text: value,
    expected: ui.original,
  });
  ui.original = value;
  ui.dirty = editor.value !== value;
  updateEditor();
  notice(
    ui.view.state.changes?.length
      ? "Saved. Review affected prose before drafting again."
      : "Saved.",
  );
}

function renderContext(content) {
  const n = node(),
    chain = ancestors(n.id),
    c = ui.view.config;
  const inherited = (kind) => {
    for (const item of [...chain].reverse()) {
      const value = item[kind];
      if (value !== undefined && value !== null) return [value, item.title];
    }
    return [true, "Default"];
  };
  const refs = [];
  for (const origin of chain)
    for (const a of origin.attachments || [])
      refs.push({ ...a, origin: origin.title });
  content.innerHTML = `<div class="card"><div class="row spread"><h3>Automatic context</h3><button data-action="context-settings" data-write>Change selection settings</button></div>${[
    ["auto_knowledge", "World wiki selection"],
    ["auto_outline", "Outline summaries"],
  ]
    .map(([key, label]) => {
      const [value, origin] = inherited(key);
      return `<div class="list-row"><div>${label}<small>Inherited from ${esc(origin)}</small></div>${badge(value ? "On" : "Off")}</div>`;
    })
    .join(
      "",
    )}</div><div class="card"><div class="row spread"><h3>Required references</h3><button data-action="attach" data-write>Attach reference</button></div><p class="muted small">These references are always included, even when automatic selection is off.</p>${refs.length ? refs.map((a) => `<div class="list-row"><div>${esc(title(a.id))}<small>${c.knowledge[a.id] ? "World entry" : "Outline"}${a.descendants ? " and descendants" : ""} · from ${esc(a.origin)}</small></div></div>`).join("") : '<p class="muted small">No references attached yet.</p>'}</div><div class="card"><div class="row spread"><h3>Your instructions to each stage</h3><button data-action="instructions" data-write>Edit instructions</button></div><p class="muted small">Add direction for outlining, context selection, writing, or review. Instructions inherit down the tree.</p>${Object.entries(
    n.prompts || {},
  )
    .filter(([, text]) => text)
    .map(
      ([role, text]) =>
        `<details><summary>${esc(roles[role] || role)}</summary><pre>${esc(text)}</pre></details>`,
    )
    .join(
      "",
    )}</div><button data-action="guidance" data-write>Model choices for this outline</button>`;
  renderShell();
}
async function renderHistory(content, id, load) {
  const [runs, versions] = await Promise.all([
    api("/api/runs"),
    api(`/api/history/${id}`),
  ]);
  if (load !== ui.load) return;
  content.innerHTML = `<h3>Generated work</h3>${
    runs
      .filter((r) => r.target === id)
      .map(
        (r) =>
          `<div class="list-row"><div>${esc(r.kind)} · ${esc(r.status)}<small>${esc(new Date(r.started).toLocaleString())}</small></div><button data-run="${esc(r.id)}">Inspect result</button></div>`,
      )
      .join("") || '<p class="muted">No generated work for this item yet.</p>'
  }<hr><h3>Source versions</h3>${(versions || []).map((v) => `<div class="list-row"><div>${esc(v.Label)}<small>${esc(new Date(v.At).toLocaleString())}</small></div><button data-version="${esc(v.ID)}">Read previous version</button></div>`).join("") || '<p class="muted">Previous text is kept here when you save an edit.</p>'}`;
}

function showDialog(titleText, html) {
  $("#dialog-title").textContent = titleText;
  $("#dialog-body").innerHTML = html;
  ui.dialogDirty = false;
  if (!$("#dialog").open) $("#dialog").showModal();
}
function closeDialog() {
  if (ui.dialogDirty && !confirm("Discard changes in this dialog?")) return;
  ui.dialogDirty = false;
  ui.wizard = null;
  $("#dialog").close();
}
function bindForm(onSubmit) {
  const f = $("form", $("#dialog-body"));
  f.addEventListener("input", () => (ui.dialogDirty = true));
  f.addEventListener("submit", async (event) => {
    event.preventDefault();
    const button = $("button[type=submit]", f);
    const label = button?.textContent;
    if (button) { button.disabled = true; button.textContent = button.dataset.busyLabel || "Working…"; }
    try {
      await onSubmit(new FormData(f));
      if (!f.isConnected) return;
      ui.dialogDirty = false;
      $("#dialog").close();
      await renderMain();
    } catch (e) {
      report(e);
    } finally {
      if (button) { button.disabled = false; button.textContent = label; }
    }
  });
}
function confirmChoice(titleText, text, choices) {
  return new Promise((resolve) => {
    showDialog(
      titleText,
      `<p>${esc(text)}</p><div class="actions end">${choices.map((c, i) => `<button class="${c.primary ? "primary" : ""}" data-answer="${i}">${esc(c.label)}</button>`).join("")}</div>`,
    );
    const dialog = $("#dialog");
    const finish = (value) => {
      dialog.removeEventListener("close", onClose);
      dialog.close();
      resolve(value);
    };
    const onClose = () => resolve("cancel");
    dialog.addEventListener("close", onClose, { once: true });
    $$("[data-answer]", dialog).forEach(
      (b) =>
        (b.onclick = () => finish(choices[Number(b.dataset.answer)].value)),
    );
  });
}

async function addChild() {
  if (!requireSaved()) return;
  const parent = node() ? ui.selected : ui.view.config.root;
  const prose = (await api(`/api/source/${parent}/prose`)).text;
  showDialog(
    `Add outline below ${title(parent)}`,
    `<form><div class="field"><label for="new-title">Title</label><input id="new-title" name="title" required autofocus placeholder="A scene, a beat, or a more specific detail"></div><div class="field"><label for="new-body">Outline</label><textarea id="new-body" name="text" rows="6" placeholder="What needs to happen here?"></textarea></div>${!ui.view.config.nodes[parent].children?.length && prose ? `<div class="hint"><p>This passage already has prose. Adding a child turns it into a parent. What should happen to the current text?</p><select name="handling" aria-label="Existing prose"><option value="outline">Keep as outline reference</option><option value="note">Keep as a private note</option><option value="discard">Remove its active contribution (history retained)</option></select></div>` : ""}<div class="actions end"><button type="submit" class="primary">Add outline</button></div></form>`,
  );
  bindForm(async (f) => {
    const id = await command("child", {
      target: parent,
      title: f.get("title"),
      text: f.get("text"),
      handling: f.get("handling") || "",
    });
    ui.selected = id;
    ui.field = "outline";
    ui.section = "outline";
  });
}
function addEntry() {
  if (!requireSaved()) return;
  showDialog(
    "New world entry",
    `<form><div class="two-col"><div class="field"><label for="entry-title">Name</label><input id="entry-title" name="title" required autofocus placeholder="A person, place, or part of your world"></div><div class="field"><label for="entry-kind">Kind</label><input id="entry-kind" name="kind" list="entry-kinds" value="World"><datalist id="entry-kinds"><option>Character</option><option>Place</option><option>World</option><option>Object</option><option>History</option></datalist></div></div><div class="field"><label for="entry-text">What should the story know?</label><textarea id="entry-text" name="text" rows="8"></textarea></div><div class="actions end"><button type="submit" class="primary">Create entry</button></div></form>`,
  );
  bindForm(async (f) => {
    ui.selected = await command("entry", {
      title: f.get("title"),
      kind: f.get("kind"),
      text: f.get("text"),
    });
    ui.section = "knowledge";
    ui.field = "entry";
  });
}
function renameItem() {
  if (!requireSaved()) return;
  const v = ui.view,
    c = structuredClone(v.config),
    id = ui.selected;
  showDialog(
    "Rename item",
    `<form><div class="field"><label for="rename-title">Title</label><input id="rename-title" name="title" required value="${esc(title(id))}"></div><div class="actions end"><button type="submit" class="primary">Save name</button></div></form>`,
  );
  bindForm(async (f) => {
    (c.nodes[id] || c.knowledge[id]).title = f.get("title");
    if (id === c.root) c.title = f.get("title");
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Renamed item",
      target: id,
    });
  });
}

function generationDialog(force = false, invalidate = false) {
  if (!requireSaved()) return;
  if (!invalidate && !configured()) {
    notice("Connect a base model before drafting.");
    navigate("models");
    return;
  }
  if (!invalidate && (ui.view.state.changes || []).length) {
    reviewChanges();
    return;
  }
  const id = node() ? ui.selected : ui.view.config.root;
  const label = invalidate
    ? "Mark for regeneration"
    : force
      ? "Generate replacements"
      : "Generate prose";
  showDialog(
    label,
    `<form><p class="muted">${invalidate ? "Existing text stays available. Authored prose remains protected." : "Each passage is drafted and reviewed against its context and style. Authored prose is kept until you explicitly choose a replacement."}</p><div class="field"><label for="generation-scope">Which passages?</label><select id="generation-scope" name="scope"><option value="branch">${esc(title(id))} (${leaves(id).length} passage${leaves(id).length === 1 ? "" : "s"})</option><option value="selected">Choose individual passages</option><option value="story">Entire story (${leaves().length} passages)</option></select></div><div id="passage-options" class="checklist" hidden>${leaves()
      .map(
        (ref) =>
          `<label class="check"><input type="checkbox" name="passage" value="${ref}">${esc(title(ref))}</label>`,
      )
      .join(
        "",
      )}</div>${!invalidate ? `<p class="form-note">This queue is limited to ${ui.view.config.limits.calls} model calls and ${ui.view.config.limits.minutes} minutes. Reviews and revisions count toward that budget.</p>` : ""}<div class="actions end"><button type="submit" class="primary">${label}</button></div></form>`,
  );
  $("#generation-scope").onchange = (e) =>
    ($("#passage-options").hidden = e.target.value !== "selected");
  bindForm(async (f) => {
    const ids = f.getAll("passage");
    if (f.get("scope") === "selected" && !ids.length)
      throw new Error("Choose at least one passage.");
    await command(invalidate ? "invalidate" : "generate", {
      target: id,
      scope: f.get("scope"),
      ids,
      force,
    });
    if (!invalidate) {
      ui.section = "activity";
      notice("Generation started. You can inspect progress in Activity.");
    }
  });
}
function expandOutline() {
  if (!requireSaved()) return;
  if (!configured()) {
    navigate("models");
    return;
  }
  const id = ui.selected;
  showDialog(
    "Develop this outline",
    `<form><p class="muted">The result will be a proposal. Your outline changes only when you import it.</p><div class="field"><label for="outline-direction">What would you like to develop?</label><textarea id="outline-direction" name="text" rows="5">Add useful detail to this brief. Preserve my existing plot decisions.</textarea></div><div class="field"><label for="outline-model">Model</label><select id="outline-model" name="model">${modelOptions("", "Use this outline’s inherited model")}</select></div><div class="actions end"><button type="submit" class="primary">Generate outline proposal</button></div></form>`,
  );
  bindForm(async (f) => {
    await command("outline", {
      target: id,
      text: f.get("text"),
      model: f.get("model"),
    });
    ui.section = "activity";
  });
}

function reviewChanges() {
  if (!requireSaved()) return;
  const changes = ui.view.state.changes || [];
  showDialog(
    "Review saved changes",
    `<p class="muted">A change can affect one passage or the whole story. Choose what to revisit; current text stays available.</p>${changes.map((c) => `<div class="card"><h3>${esc(title(c.target))}</h3><p class="muted small">${esc(c.description)}</p><div class="actions"><button data-decision="${c.id}" data-choice="keep">Keep current prose</button><button data-decision="${c.id}" data-choice="branch">${ui.view.config.knowledge[c.target] ? "Revisit linked passages" : "Revisit this branch"}</button><button data-decision="${c.id}" data-choice="selected">Choose passages</button><button data-decision="${c.id}" data-choice="story">Revisit whole story</button></div></div>`).join("")}<div class="actions end"><button data-decision="all" data-choice="keep">Keep prose for all changes</button><button data-decision="all" data-choice="story">Revisit whole story for all</button></div>`,
  );
}

function guidanceDialog() {
  if (!requireSaved()) return;
  const v = ui.view,
    c = structuredClone(v.config),
    n = c.nodes[ui.selected];
  showDialog(
    "Style inheritance and model choices",
    `<form><label class="check"><input name="replace" type="checkbox" ${n.replace_style ? "checked" : ""}>Replace inherited style with this outline’s local guide</label><p class="muted small">Otherwise, the local guide adds to its ancestors. Model choices below inherit to descendants.</p>${Object.entries(
      roles,
    )
      .map(([role, label]) => {
        const [id, origin] = resolvedModel(n.id, role);
        return `<div class="field"><label for="role-${role}">${label}</label><select id="role-${role}" name="${role}">${modelOptions(n.models?.[role] || "", "Inherit", needsTools(role))}</select><small>Currently ${esc(c.models[id]?.name || id)} · from ${esc(origin)}</small>${inferenceFields("node-"+role,n.inference?.[role],c.models[id])}</div>`;
      })
      .join(
        "",
      )}<div class="actions end"><button type="submit" class="primary">Save guidance</button></div></form>`,
  );
  bindForm(async (f) => {
    n.replace_style = f.has("replace");
    n.models = {};
    n.inference = {};
    Object.keys(roles).forEach(r => { n.models[r] = f.get(r); n.inference[r] = readInference(f,"node-"+r); });
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Changed inherited guidance",
      target: n.id,
    });
  });
}
function contextSettings() {
  const v = ui.view,
    c = structuredClone(v.config),
    n = c.nodes[ui.selected];
  showDialog(
    "Automatic context selection",
    `<form><p class="muted">Required references are included regardless of these settings.</p>${[
      ["auto_knowledge", "Find relevant world entries"],
      ["auto_outline", "Summarize relevant outlines"],
    ]
      .map(
        ([key, label]) =>
          `<div class="field"><label for="${key}">${label}</label><select id="${key}" name="${key}">${[
            ["", "Inherit"],
            ["true", "On"],
            ["false", "Off"],
          ]
            .map(
              ([value, text]) =>
                `<option value="${value}" ${String(n[key] ?? "") === value ? "selected" : ""}>${text}</option>`,
            )
            .join("")}</select></div>`,
      )
      .join(
        "",
      )}<div class="actions end"><button type="submit" class="primary">Save context settings</button></div></form>`,
  );
  bindForm(async (f) => {
    for (const key of ["auto_knowledge", "auto_outline"]) {
      if (f.get(key) === "") delete n[key];
      else n[key] = f.get(key) === "true";
    }
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Changed automatic context",
      target: n.id,
    });
  });
}
function attachReferences() {
  const v = ui.view,
    c = structuredClone(v.config),
    n = c.nodes[ui.selected];
  const refs = [
    ...Object.values(c.knowledge),
    ...Object.values(c.nodes),
  ].filter((x) => x.id !== n.id);
  showDialog(
    "Required references for this outline",
    `<form><p class="muted small">Choose world entries or other outlines to support continuity. Inherited references are changed at their source.</p><div class="checklist">${refs
      .map((ref) => {
        const attached = (n.attachments || []).find((a) => a.id === ref.id);
        return `<div class="list-row"><label class="check"><input type="checkbox" name="reference" value="${ref.id}" ${attached ? "checked" : ""}>${esc(ref.title)} <span class="muted small"> &nbsp;${ref.kind ? esc(ref.kind) : "Outline"}</span></label>${c.nodes[ref.id]?.children?.length ? `<label class="check small"><input type="checkbox" name="descendants" value="${ref.id}" ${attached?.descendants ? "checked" : ""}>Include children</label>` : ""}</div>`;
      })
      .join(
        "",
      )}</div><div class="actions end"><button type="submit" class="primary">Save references</button></div></form>`,
  );
  bindForm(async (f) => {
    n.attachments = f
      .getAll("reference")
      .map((id) => ({ id, descendants: f.getAll("descendants").includes(id) }));
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Changed required references",
      target: n.id,
    });
  });
}
function instructions() {
  const v = ui.view,
    c = structuredClone(v.config),
    n = c.nodes[ui.selected];
  showDialog(
    "Instructions for each writing stage",
    `<form><p class="muted">These additions are inherited by descendants. Use them to clarify requirements or correct recurring mistakes.</p>${Object.entries(
      roles,
    )
      .map(
        ([role, label]) =>
          `<div class="field"><label for="prompt-${role}">${label}</label><textarea id="prompt-${role}" name="${role}" rows="3">${esc(n.prompts?.[role] || "")}</textarea></div>`,
      )
      .join(
        "",
      )}<div class="actions end"><button type="submit" class="primary">Save instructions</button></div></form>`,
  );
  bindForm(async (f) => {
    n.prompts = {};
    for (const role of Object.keys(roles)) n.prompts[role] = f.get(role);
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Changed stage instructions",
      target: n.id,
    });
  });
}

function catalogStatus(state) {
  if (state?.refreshing) return "Refreshing model list…";
  if (state?.error) return state.error + (state.updated ? " · keeping the last successful list" : "");
  return state?.updated ? `Last refreshed ${new Date(state.updated).toLocaleTimeString()} · ${state.catalog.models.length} models` : "Waiting for the first model refresh…";
}

function renderModels() {
  const c = ui.view.config;
  $("#main").innerHTML = `<div class="page-head"><div class="row spread"><h2>Models</h2><button class="primary" data-action="connect-model" data-write>Add API connection</button></div><p>Connect each API once. Its models appear in the menus throughout your project.</p></div>
    <div class="card"><h3>Base model</h3><p class="muted small">Used for all tasks until you assign another model. Tool use is required. If a model is missing here, open its Model settings below to test or explicitly enable tools.</p><div class="row"><select id="base-model" aria-label="Base model">${modelOptions(c.base_model, "Choose a base model", true)}</select><button id="save-base" data-write>Use as base model</button></div></div>
    ${Object.entries(c.connections || {}).map(([id, con]) => {
      const state = ui.view.catalogs?.[id];
      const list = Object.entries(c.models).filter(([,m]) => m.connection === id && m.model);
      return `<article class="card"><div class="row spread"><h3>${esc(con.name)}</h3><span>${esc(con.provider || state?.catalog?.provider || "API")}</span></div><p class="small muted">${esc(con.url)}${con.key_env ? ` · authentication: ${esc(con.key_env)}` : " · no authentication"}</p><p class="small" role="status" id="catalog-status-${esc(id)}">${esc(catalogStatus(state))}</p>${state?.error && state?.updated ? '<p class="small muted">Showing the last successful list. Saved assignments remain available.</p>' : ""}<div class="row"><select id="connection-model-${esc(id)}" aria-label="Models from ${esc(con.name)}">${list.length ? list.map(([ref,m]) => `<option value="${esc(ref)}">${esc(m.name || m.model)}${modelAvailability(m)}</option>`).join("") : '<option value="">No writing models listed</option>'}</select><button data-model-settings-from="${esc(id)}" ${list.length ? "" : "disabled"} data-write>Model settings</button></div><div class="actions"><button data-refresh-connection="${esc(id)}" ${state?.refreshing ? "disabled" : ""}>Refresh models</button><button data-connect="${esc(id)}" data-write>Edit connection</button></div></article>`;
    }).join("")}
    ${!Object.keys(c.connections || {}).length ? '<div class="empty">Add an API connection to discover its models.</div>' : ""}
    <div class="card"><div class="row spread"><h3>Models for individual tasks</h3><button data-action="feature-models" data-write>Change assignments</button></div><p class="muted small">Each task can have its own model, reasoning setting, and output limit. Outline settings inherit and can override these choices.</p>${Object.entries(roles).map(([role,label]) => { const m = c.models[c.feature_models?.[role] || c.base_model]; return `<div class="list-row"><span>${label}</span><span class="muted small">${esc(m?.name || "Not configured")}${c.feature_models?.[role] ? "" : " · base"}</span></div>`; }).join("")}</div>`;
  $("#save-base").onclick = async () => {
    try {
      const next = structuredClone(ui.view.config);
      next.base_model = $("#base-model").value;
      if (!next.base_model) throw new Error("Choose a base model first.");
      await command("config", {config:next, expected:ui.view.configVersion, title:"Changed base model", target:next.root});
      renderModels();
    } catch(err) { report(err); }
  };
  renderShell();
}

function modelAvailability(m) {
  const state = ui.view.catalogs?.[m.connection];
  if (state?.updated && !state.catalog.models.some(found => found.id === m.model)) return " · not currently listed";
  return m.tools_unverified ? " · test tools to use as base" : !m.tools ? " · text only" : "";
}

function connectModel(id = "") {
  if (!requireSaved()) return;
  const v = ui.view, existing = v.config.connections?.[id] || {};
  showDialog(id ? "Edit API connection" : "Add API connection", `<form><div class="field"><label for="api-label">Connection name</label><input id="api-label" name="name" required placeholder="Office LM Studio" value="${esc(existing.name || "")}"></div><div class="field"><label for="api-url">API URL</label><input id="api-url" name="url" required placeholder="http://localhost:1234" value="${esc(existing.url || "")}"><small>Use an address reachable from the machine running Iterauthor.</small></div><div class="field"><label for="api-key-env">API key environment variable (optional)</label><input id="api-key-env" name="key_env" placeholder="OPENAI_API_KEY" value="${esc(existing.key_env || "")}" autocomplete="off"><small>Each connection can use its own account. The key stays on the server.</small></div><details><summary>API compatibility</summary><div class="field"><label for="api-provider">Server type</label><select id="api-provider" name="provider">${["", "LM Studio", "Ollama", "llama.cpp", "OpenAI", "Compatible API"].map(p => `<option value="${p}" ${existing.provider === p ? "selected" : ""}>${p || "Detect from API"}</option>`).join("")}</select><small>Detection reads model metadata. Override this only when your proxy hides the server type.</small></div></details><p class="muted small">Model lists refresh every minute and can be refreshed manually. Connecting only reads the catalog; it does not run the models.</p><div class="actions end"><button type="submit" class="primary" data-busy-label="Saving connection and refreshing…">Save connection and find models</button></div></form>`);
  bindForm(async f => {
    await command("save-connection", {id, expected:v.configVersion, api:{name:f.get("name").trim(),url:f.get("url").trim(),key_env:f.get("key_env").trim(),provider:f.get("provider")}});
    renderModels();
  });
}

function reasoningOptions(selected = "", inherit = "Server default", model = null) {
  let options = model?.reasoning_options?.length ? [...model.reasoning_options] : ["off", "on", "minimal", "low", "medium", "high", "xhigh"];
  if (selected && !["default", ...options].includes(selected)) options.push(selected);
  return `<option value="">${esc(inherit)}</option>` + (inherit !== "Server default" ? '<option value="default" '+(selected === "default" ? "selected" : "")+'>Server default</option>' : "") + options.map(value => `<option value="${esc(value)}" ${value === selected ? "selected" : ""}>${esc(value === "off" || value === "none" ? "Off" : value === "on" ? "On" : value[0].toUpperCase()+value.slice(1))}</option>`).join("");
}

function inferenceFields(prefix, values = {}, model = null) {
  const mode = values.output_tokens === undefined ? "inherit" : values.output_tokens === 0 ? "unlimited" : values.output_tokens === -1 ? "automatic" : "limited";
  return `<div class="two-col"><div class="field"><label for="${prefix}-reasoning">Reasoning</label><select id="${prefix}-reasoning" name="${prefix}-reasoning">${reasoningOptions(values.reasoning || "", "Inherit", model)}</select></div><div class="field"><label for="${prefix}-output-mode">Output limit</label><select id="${prefix}-output-mode" name="${prefix}-output-mode" data-output-mode="${prefix}">${["inherit","automatic","unlimited","limited"].map(v => `<option value="${v}" ${mode === v ? "selected" : ""}>${v === "inherit" ? "Inherit" : v === "automatic" ? "Automatic (fit available context)" : v === "unlimited" ? "Unlimited (no app cap)" : "Custom limit"}</option>`).join("")}</select><input id="${prefix}-output-tokens" name="${prefix}-output-tokens" aria-label="Custom output tokens" type="number" min="64" max="1048576" value="${values.output_tokens > 0 ? values.output_tokens : 16384}" ${mode === "limited" ? "" : "hidden disabled"}></div></div>`;
}
function readInference(f, prefix) {
  const values = {}, mode = f.get(`${prefix}-output-mode`);
  if (mode === "unlimited") values.output_tokens = 0;
  if (mode === "automatic") values.output_tokens = -1;
  if (mode === "limited") values.output_tokens = Number(f.get(`${prefix}-output-tokens`));
  if (f.get(`${prefix}-reasoning`)) values.reasoning = f.get(`${prefix}-reasoning`);
  return values;
}

function modelSettings(id) {
  const v = ui.view, m = structuredClone(v.config.models[id]);
  if (!m) return;
  showDialog(`Model settings · ${m.name || m.model}`, `<form><p class="muted small">${esc(v.config.connections?.[m.connection]?.name || m.url)} · ${esc(m.model)}${m.context ? ` · ${esc(m.context_source || "reported")} context: ${m.context.toLocaleString()} tokens` : ""}</p><div class="field"><label for="model-name">Name in this project</label><input id="model-name" name="name" value="${esc(m.name || m.model)}" required></div>${inferenceFields("model", m, m)}<p class="muted small">Inherit uses the project output limit and the server’s default reasoning. Automatic reserves estimated input space and adjusts each output allowance. Unlimited omits the output cap parameter; the server’s default may still impose a limit. Reasoning levels depend on the model. ${m.reasoning_options?.length ? "The listed reasoning options were reported by the server." : "This server does not report reasoning options; use its default unless you know which options the model supports."}</p><details><summary>Compatibility and tool support</summary><label class="check"><input name="tools" type="checkbox" ${m.tools ? "checked" : ""}>Allow use in tasks requiring tools</label><small>${m.tools_unverified ? "Tool capability has not been verified. The optional test checks a complete tool exchange." : "You can verify tool support with the optional test below."}</small><div class="field"><label for="token-field">Output limit parameter</label><select id="token-field" name="token_field"><option value="">API default</option>${["max_tokens","max_completion_tokens"].map(f => `<option ${m.token_field === f ? "selected" : ""}>${f}</option>`).join("")}</select></div><div class="field"><label for="reasoning-field">Reasoning parameter</label><select id="reasoning-field" name="reasoning_field"><option value="">API default</option><option value="reasoning_effort" ${m.reasoning_field === "reasoning_effort" ? "selected" : ""}>reasoning_effort</option><option value="chat_template_kwargs" ${m.reasoning_field === "chat_template_kwargs" ? "selected" : ""}>Chat template (llama.cpp / vLLM)</option></select></div></details><div id="probe-status" class="form-note" role="status"></div><div class="actions end"><button type="button" id="probe-model">Test model</button><button type="submit" class="primary">Save model settings</button></div></form>`);
  let testResult;
  $("#dialog form").addEventListener("change", event => {
    if (["model-reasoning", "reasoning-field", "token-field"].includes(event.target.id)) {
      testResult = undefined;
      $("#probe-status").textContent = "Settings changed since the last test. Test again to verify this configuration.";
    }
  });
  const candidate = f => ({...m, name:f.get("name"), tools:f.has("tools"), tools_override:m.tools_override, token_field:f.get("token_field"), reasoning_field:f.get("reasoning_field"), reasoning:"", output_tokens:undefined, ...readInference(f,"model")});
  $("#probe-model").onclick = async () => {
    const button = $("#probe-model"), status = $("#probe-status");
    button.disabled = true; status.textContent = "Testing text and a complete tool exchange…";
    try {
      const value = candidate(new FormData($("#dialog form")));
      testResult = undefined;
      const result = await api("/api/models/probe", value);
      if (!button.isConnected) return;
      testResult = result;
      status.textContent = result.detail;
      $("#dialog [name=tools]").checked = result.tools;
      $("#token-field").value = result.model.token_field;
    } catch(err) { status.textContent = err.message; }
    finally { button.disabled = false; await refresh(); }
  };
  bindForm(async f => {
    const value = candidate(f);
    if (testResult || value.tools !== m.tools || (m.tools_unverified && value.tools)) {
      value.tools_override = value.tools;
      value.tools_unverified = false;
    }
    await command("save-model", {id, connection:value, expected:v.configVersion});
    renderModels();
  });
}

function featureModels() {
  const v = ui.view,
    c = structuredClone(v.config);
  showDialog(
    "Models for individual tasks",
    `<form><p class="muted">Leave a task on “Use base model” unless you want a different model’s strengths.</p>${Object.entries(
      roles,
    )
      .map(
        ([role, label]) =>
          `<div class="field"><label for="feature-${role}">${label}</label><select id="feature-${role}" name="${role}">${modelOptions(c.feature_models?.[role] || "", "Use base model", needsTools(role))}</select>${inferenceFields("feature-"+role,c.feature_inference?.[role],c.models[c.feature_models?.[role] || c.base_model])}</div>`,
      )
      .join(
        "",
      )}<div class="actions end"><button type="submit" class="primary">Save assignments</button></div></form>`,
  );
  bindForm(async (f) => {
    c.feature_models = {};
    c.feature_inference = {};
    for (const role of Object.keys(roles)) { c.feature_models[role] = f.get(role); c.feature_inference[role] = readInference(f,"feature-"+role); }
    await command("config", {
      config: c,
      expected: v.configVersion,
      title: "Changed feature models",
      target: c.root,
    });
  });
}
function renderSettings() {
  ui.settingsDirty = false;
  const c = ui.view.config;
  $("#main").innerHTML =
    `<div class="page-head"><h2>Project settings</h2><p>Limits and workflow preferences for ${esc(c.title)}.</p></div><div class="card"><h3>Generation limits</h3><p class="muted small">These limits cover drafting, context selection, tools, and reviews together.</p><form id="settings-form"><div class="two-col">${[
      ["drafts", "Draft attempts per passage", 1, 10],
      ["calls", "Model calls per queue", 1, 200],
      ["minutes", "Minutes per queue (0 = unlimited)", 0, 240],
      ["context_chars", "Input characters per call", 1000, 1000000],
    ]
      .map(
        ([key, label, min, max]) =>
          `<div class="field"><label for="limit-${key}">${label}</label><input id="limit-${key}" name="${key}" type="number" min="${min}" max="${max}" required value="${c.limits[key]}"></div>`,
      )
      .join(
        "",
      )}</div><div class="field"><label for="project-output-mode">Output tokens per call</label><select id="project-output-mode" name="project-output-mode" data-output-mode="project"><option value="automatic" ${c.limits.output_tokens === -1 ? "selected" : ""}>Automatic (fit available context)</option><option value="unlimited" ${c.limits.output_tokens === 0 ? "selected" : ""}>Unlimited (no app cap)</option><option value="limited" ${c.limits.output_tokens > 0 ? "selected" : ""}>Custom limit</option></select><input id="project-output-tokens" name="project-output-tokens" aria-label="Custom output tokens" type="number" min="64" max="1048576" value="${c.limits.output_tokens > 0 ? c.limits.output_tokens : 16384}" ${c.limits.output_tokens > 0 ? "" : "hidden disabled"}><small>Automatic budgets by task and reported context size, with an estimated input allowance. Unlimited leaves output length to the model server; its configured limit still applies. Model and task settings can override this limit.</small></div><label class="check"><input name="automatic" type="checkbox" ${c.generate_after_editing ? "checked" : ""}>Draft missing or invalidated prose when I finish editing</label><div class="actions"><button type="submit" class="primary" data-write>Save settings</button><button type="button" data-action="finish-editing" data-write>Finish editing</button></div></form></div><div class="card"><h3>Project files</h3><p class="muted small"><code>${esc(ui.view.directory)}</code></p><p class="small muted">Your outlines and wiki remain Markdown files. Pause generation before external edits, then reload.</p><button data-action="reload" data-write>Reload project files</button></div>`;
  const form = $("#settings-form");
  form.dataset.version = ui.view.configVersion;
  form.addEventListener("input", () => (ui.settingsDirty = true));
  form.onsubmit = async (event) => {
    event.preventDefault();
    try {
      await saveSettings();
    } catch (err) {
      report(err);
    }
  };
  renderShell();
}
async function saveSettings() {
  const form = $("#settings-form");
  if (!form || !form.reportValidity())
    throw new Error("Check the settings values.");
  const f = new FormData(form),
    next = structuredClone(ui.view.config);
  for (const key of Object.keys(next.limits))
    next.limits[key] = Number(f.get(key));
  next.limits.output_tokens = f.get("project-output-mode") === "unlimited" ? 0 : f.get("project-output-mode") === "automatic" ? -1 : Number(f.get("project-output-tokens"));
  next.generate_after_editing = f.has("automatic");
  await command("config", {
    config: next,
    expected: form.dataset.version,
    title: "Changed generation limits",
    target: next.root,
  });
  ui.settingsDirty = false;
  renderSettings();
  notice("Project settings saved.");
}

function callBudgets(run) {
  const calls = (run.trace || []).filter(t => !t.tool && t.response?.budget);
  if (!calls.length) return "";
  return `<details open><summary>Settings used for each call</summary>${calls.map(t => {
    const b=t.response.budget, r=t.response;
    return `<div class="review"><strong>${esc(t.stage)} · ${esc(ui.view.config.models[t.model]?.name || t.model)}</strong><p class="small">Reasoning: ${esc(t.options?.reasoning || "server default")} · output: ${b.output_tokens === 0 ? "Unlimited (server default)" : b.output_tokens.toLocaleString()+" tokens"}${b.requested === -1 ? " (Automatic)" : b.requested > b.output_tokens ? ` (requested ${b.requested.toLocaleString()})` : ""}<br>Estimated input: ${b.estimated_input_tokens.toLocaleString()} tokens${b.context ? ` · ${esc(b.context_source || "reported")} context: ${b.context.toLocaleString()}` : ""}</p><p class="small muted">${esc(b.note)}</p>${r.input_tokens !== undefined || r.output_tokens !== undefined ? `<p class="small">Server usage: ${r.input_tokens ?? "unknown"} input · ${r.output_tokens ?? "unknown"} output${r.reasoning_tokens !== undefined ? ` · ${r.reasoning_tokens} reasoning` : ""}</p>` : ""}</div>`;
  }).join("")}</details>`;
}

async function inspectRun(id) {
  if (!requireSaved()) return;
  const run = await api(`/api/runs/${id}`);
  const invalid = ui.view.state.invalid_runs?.[id];
  const review = (name, r) =>
    `<div class="review"><h4>${name} · ${r ? (r.pass ? "Passed" : "Needs revision") : "Not completed"}</h4>${r ? `<ul>${[...(r.issues || []), ...(r.suggestions || [])].map((text) => `<li>${esc(text)}</li>`).join("")}</ul>` : ""}</div>`;
  let edits = "";
  for (const edit of run.edits || []) {
    const current = await api(`/api/source/${edit.id}/${edit.field}`);
    edits += `<h3>${esc(title(edit.id))} / ${esc(edit.field)}</h3><div class="before-after"><section><h4>Current</h4><pre>${esc(current.text)}</pre></section><section><h4>Proposed</h4><pre>${esc(edit.text)}</pre></section></div>`;
  }
  showDialog(
    `${title(run.target)} · ${run.kind}`,
    `<div class="row spread">${badge(invalid ? "Invalidated" : run.status)}<span class="muted small">${run.calls} calls · ${run.reported_tokens} reported tokens</span></div>${run.error ? `<p class="error-box">${esc(run.error)}</p>` : ""}${run.demo ? '<p class="form-note">Synthetic demo output and reviews.</p>' : ""}${run.text ? `<div class="prose-preview" style="margin-top:20px">${esc(run.text)}</div>` : ""}${(run.candidates || []).map((candidate, index) => `<section class="card" style="margin-top:20px"><h3>Candidate ${index + 1}</h3><div class="prose-preview">${esc(candidate.text)}</div>${review("Consistency", candidate.consistency)}${review("Style", candidate.style)}<button class="primary" data-use-run="${id}" data-index="${index}" ${invalid || ui.view.busy ? "disabled" : ""}>Use this candidate</button></section>`).join("")}${edits}<div class="actions" style="margin-top:20px">${run.kind === "outline" && run.text ? `<button class="primary" data-import="${id}" ${invalid || ui.view.busy ? "disabled" : ""}>Import into outline</button>` : ""}${run.edits?.length ? `<button class="primary" data-apply="${id}" ${invalid || ui.view.busy ? "disabled" : ""}>Apply proposed edits</button>` : ""}<button data-invalidate-run="${id}" ${invalid || ui.view.busy ? "disabled" : ""}>Invalidate this result</button></div>${callBudgets(run)}<details><summary>Exact context, prompts, and tool calls</summary><pre>${esc(run.context || "")}\n\n${esc(JSON.stringify(run.trace || [], null, 2))}</pre></details>`,
  );
}

function renderAssistant() {
  const focused = document.activeElement?.id === "chat-input",
    selection = focused
      ? [$("#chat-input").selectionStart, $("#chat-input").selectionEnd]
      : null;
  const box = $("#assistant");
  box.hidden = !ui.assistant;
  if (!ui.assistant) return;
  const c = ui.conversation;
  box.innerHTML = `<div class="assistant-head"><h3>Writing assistant</h3><button data-action="assistant" aria-label="Close writing assistant">×</button></div>${c ? `<div class="assistant-scope">${esc(c.mode === "edit" ? "Source edits" : "Discussion")} · <strong>${esc(title(c.target))}</strong><br>${esc(ui.view.config.models[c.model]?.name || c.model)}<br>${c.mode === "edit" ? "Changes are proposed for you to review and apply." : "Replies appear here; sources stay unchanged."}</div>` : '<p class="muted small">Discuss an outline, develop an idea, or ask for help diagnosing a passage.</p>'}<div class="assistant-toolbar"><button data-action="discuss" data-write>New conversation</button><button data-action="sessions">Saved conversations</button>${c ? '<button data-action="chat-model" data-write>Model</button>' : ""}</div><div class="turns" id="chat-turns">${c ? (c.turns || []).map((turn) => `<div class="turn ${turn.role === "author" ? "author" : ""}"><strong>${turn.role === "author" ? "You" : "Assistant"}</strong>${turn.status && turn.status !== "Available" ? badge(turn.status) : ""}${esc(turn.text)}${turn.run ? `<button data-run="${esc(turn.run)}">${turn.proposals ? "Review proposed edits" : "Inspect result"}</button>` : ""}${turn === c.turns.at(-1) && ["Failed", "Canceled"].includes(turn.status) ? '<button data-action="retry-message">Retry message</button><button data-nav="settings">Project settings</button>' : ""}</div>`).join("") : '<div class="empty small">Start a conversation with the outline or entry you are viewing.</div>'}</div><div id="assistant-status" role="status" aria-live="polite" hidden></div>${c ? `<form class="chat-compose" id="chat-form"><label for="chat-input" class="small">Message</label><textarea id="chat-input" rows="4" placeholder="What would you like to work on?">${esc(ui.draft)}</textarea><div class="actions"><small>Ctrl+Enter / ⌘Enter sends</small><button type="submit" class="primary" ${ui.view.busy ? "disabled" : ""}>Send</button></div></form>` : ""}`;
  if (c) {
    const input = $("#chat-input");
    input.oninput = () => {
      ui.draft = input.value;
      clearTimeout(draftTimer);
      const id = c.id,
        text = ui.draft;
      draftTimer = setTimeout(() => queueDraft(id, text).catch(report), 500);
    };
    $("#chat-form").onsubmit = async (e) => {
      e.preventDefault();
      await sendAssistant(c.id, input.value);
    };
    input.onkeydown = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
        e.preventDefault();
        $("#chat-form").requestSubmit();
      }
    };
    input.disabled = ui.sending;
    $$("[data-write]", box).forEach(
      (b) => (b.disabled = ui.view.busy || ui.sending),
    );
    const turns = $("#chat-turns");
    if (turns.lastElementChild) turns.scrollTop = turns.lastElementChild.offsetTop;
    if (focused) {
      input.focus();
      input.setSelectionRange(...selection);
    }
  }
  renderAssistantStatus();
}
async function sendAssistant(id, message) {
  if (ui.sending) return;
  const text = message.trim();
  const followUp = ui.draft.trim() === text ? "" : ui.draft;
  ui.sendError = "";
  if (!requireSaved()) ui.sendError = "Save or discard your source changes before sending.";
  else if (ui.view.busy) ui.sendError = "Wait for the current operation to finish, or stop it first.";
  else if (!text) ui.sendError = "Enter a message first.";
  if (ui.sendError) {
    renderAssistantStatus();
    return;
  }
  clearTimeout(draftTimer);
  ui.sending = true;
  ui.sendingSince = new Date().toISOString();
  renderAssistant();
  try {
    await pendingDraft;
    await api("/api/command", { action: "send", id, text });
    if (ui.conversation?.id === id) {
      ui.draft = followUp;
      ui.savedDraft = "";
      if (followUp) await queueDraft(id, followUp);
      await loadConversation(id);
    }
    await refresh();
  } catch (err) {
    if (ui.conversation?.id === id) ui.draft = followUp || text;
    ui.sendError = `Could not send or confirm the message: ${err.message}. Check the conversation before retrying.`;
    report(err);
  } finally {
    ui.sending = false;
    renderAssistant();
  }
}
async function loadConversation(id) {
  const seq = ++ui.conversationLoad;
  const c = await api(`/api/conversations/${id}`);
  if (seq !== ui.conversationLoad) return;
  if (ui.conversation?.id !== id) {
    ui.draft = c.draft || "";
    ui.sendError = "";
  }
  ui.conversation = c;
  ui.savedDraft = c.draft || "";
  renderAssistant();
}
function queueDraft(id, text) {
  const task = pendingDraft
    .then(() => api("/api/command", { action: "draft", id, text }))
    .then(() => {
      if (ui.conversation?.id === id) ui.savedDraft = text;
    });
  pendingDraft = task.catch(() => {});
  return task;
}
async function saveDraft() {
  clearTimeout(draftTimer);
  if (ui.conversation) await queueDraft(ui.conversation.id, ui.draft);
}
function newConversation() {
  if (!requireSaved()) return;
  if (!configured()) {
    navigate("models");
    return;
  }
  const target =
    node() || ui.view.config.knowledge[ui.selected]
      ? ui.selected
      : ui.view.config.root;
  const remembered =
    ui.view.state.last_manual_model || ui.view.config.base_model;
  showDialog(
    "Start a writing conversation",
    `<form><div class="hint">Scope: <strong>${esc(title(target))}</strong><br><span class="small">Edit proposals can affect this item and its outline descendants. Private notes are excluded.</span></div><div class="field" style="margin-top:20px"><label for="conversation-mode">What are we doing?</label><select id="conversation-mode" name="kind"><option value="advice">Discuss, research, or diagnose</option><option value="edit">Propose changes to the sources</option></select></div><div class="field"><label for="conversation-model">Model</label><select id="conversation-model" name="model">${modelOptions(remembered, "", true)}</select></div><div class="actions end"><button type="submit" class="primary">Start conversation</button></div></form>`,
  );
  bindForm(async (f) => {
    await saveDraft();
    const c = await command("conversation", {
      target,
      kind: f.get("kind"),
      model: f.get("model"),
    });
    ui.conversation = c;
    ui.conversationLoad++;
    ui.savedDraft = "";
    ui.draft = "";
    ui.sendError = "";
    ui.assistant = true;
    renderAssistant();
  });
}
async function sessions() {
  await saveDraft();
  const rows = await api("/api/conversations");
  showDialog(
    "Saved conversations",
    rows?.length
      ? rows
          .map(
            (c) =>
              `<div class="list-row"><div>${esc(title(c.target))}<small>${esc(c.mode)} · ${(c.turns || []).length} messages · ${esc(ui.view.config.models[c.model]?.name || c.model)}</small></div><button data-session="${esc(c.id)}">Resume</button></div>`,
          )
          .join("")
      : '<p class="muted">Start a conversation from any outline or world entry.</p>',
  );
}

document.addEventListener("change", event => {
  const select = event.target;
  const match = /^(feature|role)-(knowledge|outline-context|outline|prose|consistency|style)$/.exec(select.id || "");
  if (match) {
    const role = match[2], prefix = (match[1] === "role" ? "node-" : "feature-") + role;
    const inherited = match[1] === "role" ? resolvedModel(node()?.parent || "",role)[0] : ui.view.config.base_model;
    const reasoning = $("#"+prefix+"-reasoning");
    reasoning.innerHTML = reasoningOptions(reasoning.value,"Inherit",ui.view.config.models[select.value || inherited]);
  }
  const prefix = event.target.dataset.outputMode;
  if (prefix) {
    const input = $("#"+prefix+"-output-tokens"), limited = event.target.value === "limited";
    input.hidden = !limited; input.disabled = !limited;
  }
});
document.addEventListener("focusin", event => {
  const select = event.target;
  if (select.tagName !== "SELECT") return;
  if (select.id.startsWith("connection-model-")) {
    const connection = select.id.slice("connection-model-".length), value = select.value;
    const models = Object.entries(ui.view.config.models).filter(([,m]) => m.connection === connection && m.model);
    select.innerHTML = models.map(([id,m]) => `<option value="${esc(id)}">${esc(m.name || m.model)}${modelAvailability(m)}</option>`).join("") || '<option value="">No writing models listed</option>';
    if (models.some(([id])=>id===value)) select.value = value;
    return;
  }
  if (!select.querySelector("optgroup") && !["base-model","conversation-model","chat-model"].includes(select.id) && !/^(feature|role)-(knowledge|outline-context|outline|prose|consistency|style)$/.test(select.id)) return;
  const selected = select.value, first = select.querySelector('option[value=""]');
  const tools = ["base-model","conversation-model","chat-model"].includes(select.id) || /^(feature|role)-(knowledge|outline-context|consistency)$/.test(select.id);
  select.innerHTML = modelOptions(selected, first?.textContent || "", tools);
  if (Array.from(select.options).some(o=>o.value===selected)) select.value = selected;
});
document.addEventListener("click", async (event) => {
  const b = event.target.closest("button");
  if (!b || b.disabled) return;
  try {
    if (b.dataset.nav) {
      await navigate(b.dataset.nav);
      return;
    }
    if (b.dataset.item) {
      await navigate(b.dataset.section, b.dataset.item);
      return;
    }
    if (b.dataset.tab) {
      if (await mayNavigate()) {
        ui.field = b.dataset.tab;
        await renderDocument();
      }
      return;
    }
    if (b.dataset.run) {
      await inspectRun(b.dataset.run);
      return;
    }
    if (b.dataset.version) {
      const v = await api(
        `/api/version?id=${encodeURIComponent(b.dataset.version)}`,
      );
      showDialog("Previous source version", `<pre>${esc(v.text)}</pre>`);
      return;
    }
    if (b.dataset.connect !== undefined) {
      connectModel(b.dataset.connect);
      return;
    }
    if (b.dataset.refreshConnection) {
      b.disabled = true;
      try { await api("/api/connections/refresh", {id:b.dataset.refreshConnection}); }
      finally { await refresh(); if (ui.section === "models") renderModels(); }
      return;
    }
    if (b.dataset.modelSettingsFrom) {
      modelSettings($("#connection-model-"+b.dataset.modelSettingsFrom).value);
      return;
    }
    if (b.dataset.base) {
      const v = ui.view,
        c = structuredClone(v.config);
      c.base_model = b.dataset.base;
      await command("config", {
        config: c,
        expected: v.configVersion,
        title: "Changed base model",
        target: c.root,
      });
      renderModels();
      return;
    }
    if (b.dataset.decision) {
      const id = b.dataset.decision,
        choice = b.dataset.choice;
      if (choice === "selected") {
        showDialog(
          "Choose affected passages",
          `<form><div class="checklist">${leaves()
            .map(
              (ref) =>
                `<label class="check"><input type="checkbox" name="passage" value="${ref}">${esc(title(ref))}</label>`,
            )
            .join(
              "",
            )}</div><div class="actions end"><button type="submit" class="primary">Mark selected passages</button></div></form>`,
        );
        bindForm(async (f) => {
          const ids = f.getAll("passage");
          if (!ids.length) throw new Error("Choose at least one passage.");
          await command("decide", { id, choice, ids });
        });
        return;
      }
      await command("decide", { id, all: id === "all", choice });
      if (ui.view.state.changes?.length) reviewChanges();
      else {
        $("#dialog").close();
        notice("Change decisions saved. You can draft when ready.");
      }
      return;
    }
    for (const [key, action] of [
      ["useRun", "use-candidate"],
      ["import", "import-outline"],
      ["apply", "apply-edits"],
      ["invalidateRun", "invalidate-run"],
    ])
      if (b.dataset[key]) {
        await command(action, {
          id: b.dataset[key],
          index: Number(b.dataset.index || 0),
        });
        $("#dialog").close();
        await renderMain();
        notice(
          action === "use-candidate"
            ? "Candidate is now the active prose."
            : action === "apply-edits"
              ? "Proposed edits applied."
              : action === "import-outline"
                ? "Proposal imported as authored outline detail."
                : "Result invalidated; history retained.",
        );
        return;
      }
    if (b.dataset.session) {
      await loadConversation(b.dataset.session);
      ui.assistant = true;
      renderAssistant();
      $("#dialog").close();
      return;
    }
    switch (b.dataset.action) {
      case "dismiss-work":
        ui.dismissedWork = ui.view.work?.started || "";
        renderWorkStatus();
        break;
      case "retry-message": {
        const c = ui.conversation;
        const prompt = [...(c?.turns || [])].reverse().find((turn) => turn.role === "author");
        if (prompt) await sendAssistant(c.id, prompt.text);
        break;
      }

      case "save":
        await saveDocument();
        break;
      case "discard":
        ui.dirty = false;
        await renderDocument();
        break;
      case "generate":
        generationDialog(b.dataset.force === "true");
        break;
      case "invalidate":
        generationDialog(false, true);
        break;
      case "expand":
        expandOutline();
        break;
      case "add-child":
        await addChild();
        break;
      case "add-entry":
        addEntry();
        break;
      case "rename":
        renameItem();
        break;
      case "changes":
        reviewChanges();
        break;
      case "cancel":
        await command("cancel");
        notice("Stop requested. Editing unlocks after the current call ends.");
        break;
      case "reload":
        if (await mayNavigate()) {
          await command("reload");
          if (
            !ui.view.config.nodes[ui.selected] &&
            !ui.view.config.knowledge[ui.selected]
          ) {
            ui.selected = ui.view.config.root;
            ui.section = "outline";
            ui.field = "outline";
          }
          await renderMain();
          notice("Project files reloaded.");
        }
        break;
      case "finish-editing":
        if (requireSaved()) {
          if (ui.view.state.changes?.length) reviewChanges();
          else {
            await command("finish-editing");
            notice("Editing finished.");
          }
        }
        break;
      case "export":
        if (requireSaved()) {
          await command("export");
          const link = document.createElement("a");
          link.href = "/download/manuscript";
          link.download = "manuscript.md";
          link.click();
          notice(
            "Manuscript exported. A copy and manifest are in the project’s exports folder.",
          );
        }
        break;
      case "refresh-main":
        await refresh();
        await renderMain();
        break;
      case "guidance":
        guidanceDialog();
        break;
      case "context-settings":
        contextSettings();
        break;
      case "attach":
        attachReferences();
        break;
      case "instructions":
        instructions();
        break;
      case "connect-model":
        connectModel();
        break;
      case "feature-models":
        featureModels();
        break;
      case "assistant":
        ui.assistant = !ui.assistant;
        renderAssistant();
        break;
      case "discuss":
        newConversation();
        break;
      case "sessions":
        await sessions();
        break;
      case "chat-model": {
        const c = ui.conversation;
        showDialog(
          "Model for this conversation",
          `<form><div class="field"><label for="chat-model">Model</label><select id="chat-model" name="model">${modelOptions(c.model, "", true)}</select></div><div class="actions end"><button type="submit" class="primary">Use this model</button></div></form>`,
        );
        bindForm(async (f) => {
          await command("conversation-model", {
            id: c.id,
            model: f.get("model"),
          });
          await loadConversation(c.id);
        });
        break;
      }
    }
  } catch (e) {
    report(e);
  }
});
$("#dialog-close").onclick = closeDialog;
$("#dialog").addEventListener("cancel", (event) => {
  event.preventDefault();
  closeDialog();
});
$("#dialog").addEventListener("close", () => {
  ui.wizard = null;
  ui.dialogDirty = false;
});
document.addEventListener("keydown", (e) => {
  if (
    (e.ctrlKey || e.metaKey) &&
    e.key.toLowerCase() === "s" &&
    !$("#dialog").open
  ) {
    e.preventDefault();
    (ui.settingsDirty ? saveSettings() : saveDocument()).catch(report);
  }
});
window.addEventListener("beforeunload", (e) => {
  if (
    ui.dirty ||
    ui.dialogDirty ||
    ui.settingsDirty ||
    ui.draft !== ui.savedDraft
  ) {
    e.preventDefault();
    e.returnValue = "";
  }
});

(async () => {
  await refresh();
  const stream = new EventSource("/api/events");
  stream.addEventListener("change", () => refresh());
  stream.onopen = () => {
    ui.connectionLost = false;
    $("#connection-state").textContent = "Connected to your project";
    refresh().then(() => ui.conversation && loadConversation(ui.conversation.id)).catch(report);
  };
  stream.onerror = () => {
    ui.connectionLost = true;
    $("#connection-state").textContent = "Reconnecting… unsaved text stays here";
    renderAssistantStatus();
  };
  setInterval(() => {
    $$("[data-elapsed]").forEach((el) => (el.textContent = elapsed(el.dataset.elapsed)));
    renderAssistantStatus();
  }, 1000);
  // Poll while working or disconnected so delayed/buffered SSE cannot hide a result.
  setInterval(() => {
    if (ui.view?.busy || ui.sending || ui.connectionLost) refresh();
  }, 3000);
})().catch(report);
