import { createQueue } from "/web/queue.js";
import { register } from "/web/register.js";
import { applyOp } from "/static/ops.mjs";

const $ = (id) => document.getElementById(id);
const weekSel = $("week-select"), daySel = $("day-select"), setsEl = $("sets"), statusEl = $("status");

// state is GET /block: the active block (program tab, started, weeks) and the
// program's block tabs ("w41-46").
let state = { block: { program: "", started: "", weeks: [] }, programs: [] };
// week is the shown week tab; base is its rows (all days) as the server last
// sent them; rows is base with the queued edits of that week applied on top.
// Rows of a day not started yet are planned (from the program); days is the
// week's days in order, rest days included.
// day filters what is shown; indexes stay those of the whole week.
let week = null, day = null, base = [], rows = [], days = [];

// offlineErr marks a request that never reached the server: fetch rejected or
// the service worker answered for it (X-Offline).
function offlineErr() {
  const err = new Error("offline");
  err.offline = true;
  return err;
}

// The last /block and the last week's rows are kept so the page opens offline.
let savedCopy = false; // the last GET was answered from the kept copy
const keptKey = (path) => (path === "/block" ? "kept:block" : path.endsWith("/rows") ? "kept:rows" : null);
function keep(path, body) {
  const k = keptKey(path);
  if (k) remember(k, JSON.stringify({ path, body }));
}
function kept(path) {
  const k = keptKey(path);
  try { const v = JSON.parse(k && recall(k)); return v?.path === path ? v.body : undefined; } catch { return undefined; }
}

// API is the server's origin when the page is not served by it: the
// Capacitor app (capacitor/) sets it; on the web it is "" and paths are
// relative. native is that app: no service worker, queue in Preferences.
const API = globalThis.WORKOUTS_API || "";
const native = !!globalThis.Capacitor?.isNativePlatform?.();

// The ops queue and the token live in localStorage on the web and in Capacitor
// Preferences in the app, where the WebView's storage is not durable.
// capacitor.js, loaded by the app's index.html, gives registerPlugin;
// Preferences is native, no JS.
const prefs = native && globalThis.Capacitor.registerPlugin("Preferences");
const kv = prefs ? {
  async get(k) {
    try { return JSON.parse((await prefs.get({ key: k })).value); } catch { return null; }
  },
  async set(k, v) {
    try { await prefs.set({ key: k, value: JSON.stringify(v) }); } catch { /* storage unavailable */ }
  },
} : {
  async get(k) { return JSON.parse(recall(k)); },
  async set(k, v) { remember(k, JSON.stringify(v)); },
};

// Every route but the shell needs the token. The web gets it as the login
// cookie; the app's requests leave the WebView through native HTTP, so it
// sends the token itself, asked once on the first 401 and kept in Preferences.
let token = native ? await kv.get("token") : null;
const withToken = (opts) => (token ? { ...opts, headers: { ...opts.headers, authorization: "Bearer " + token } } : opts);

async function unauthorized() {
  if (native) {
    const t = prompt("workouts token");
    if (t) {
      await kv.set("token", t);
      location.reload();
    }
  } else {
    location.assign(API + "/login");
  }
  throw new Error("login required");
}

async function api(path, opts = {}) {
  let res = null;
  try { res = await fetch(API + path, withToken(opts)); } catch { /* network down */ }
  if (!res || res.headers.get("X-Offline") === "1") {
    const body = opts.method ? undefined : kept(path);
    if (body === undefined) throw offlineErr();
    savedCopy = true;
    return body;
  }
  if (!opts.method) savedCopy = false;
  if (res.status === 204) return null;
  if (res.status === 401) await unauthorized();
  if (!res.ok) {
    const err = new Error((await res.text()).trim() || res.statusText);
    err.status = res.status;
    throw err;
  }
  const body = await res.json();
  if (!opts.method) keep(path, body);
  return body;
}

function status(msg, error = false) {
  statusEl.textContent = msg;
  statusEl.classList.toggle("error", error);
  statusEl.classList.toggle("hidden", !msg);
}

function remember(key, value) {
  try { value == null ? localStorage.removeItem(key) : localStorage.setItem(key, value); } catch { /* storage unavailable */ }
}
function recall(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

// Done lives only on this device, not in the sheet: a set of keys per block
// and week. Keys name the set by day, exercise and number, so duplicate and
// delete re-key the week's done sets (see edit).
const doneKey = (r) => r.day + "\u0000" + r.exercise + "\u0000" + r.set;
const doneStore = () => "done:" + state.block.program + ":" + state.block.started + ":" + week;
let done = new Set();

function loadDone() {
  try { done = new Set(JSON.parse(localStorage.getItem(doneStore())) || []); } catch { done = new Set(); }
}
function saveDone() {
  try { localStorage.setItem(doneStore(), JSON.stringify([...done])); } catch { /* storage unavailable */ }
}
function markDone() {
  for (const r of rows) r.done = done.has(doneKey(r));
}

const weekPath = (w) => "/weeks/" + encodeURIComponent(w);
const rowsPath = (w) => weekPath(w) + "/rows";

function option(sel, value, label) {
  const o = document.createElement("option");
  o.value = value;
  o.textContent = label;
  sel.appendChild(o);
}

async function refresh(select) {
  state = await api("/block");
  const b = state.block;
  $("block-label").textContent = b.program ? b.program + (b.started ? ", started " + b.started : "") : "workouts";
  weekSel.textContent = "";
  for (const w of b.weeks) option(weekSel, w, w);
  weekSel.classList.toggle("hidden", !b.weeks.length);
  daySel.classList.toggle("hidden", !b.weeks.length);
  if (!b.weeks.length) {
    week = null; base = []; rows = [];
    render();
    status(state.programs.length ? "no block yet: pick one under Program" : "the program sheet has no block tabs (named like w41-46)");
    return;
  }
  const want = select || recall("week") || b.current;
  await load(b.weeks.includes(want) ? want : b.current || b.weeks[0]);
}

function fillDays() {
  const ds = days.length ? days : [...new Set(rows.map((r) => r.day))];
  daySel.textContent = "";
  for (const d of ds) option(daySel, d, d);
  // Open on today (labels start with the weekday, "SATURDAY (DEADLIFT)"),
  // else on the first day with an unticked set.
  const today = new Date().toLocaleDateString("en-US", { weekday: "long" }).toUpperCase();
  if (!ds.includes(day)) day = ds.find((d) => d.toUpperCase().startsWith(today))
    || ds.find((d) => rows.some((r) => r.day === d && !r.done)) || ds[0] || null;
  daySel.value = day ?? "";
}

// load shows week w.
async function load(w) {
  week = w;
  remember("week", w);
  loadDone();
  weekSel.value = w;
  status("loading");
  let got;
  try {
    got = await api(rowsPath(w));
  } catch (e) {
    if (!e.offline) throw e;
    base = []; days = [];
    overlay();
    status("offline and no saved copy of this week", true);
    return;
  }
  if (w !== week) return;
  setView(got);
  status(savedCopy ? "offline: showing the last saved copy" : got.warning || (rows.length ? "" : "this week has no sets"), !!got.warning);
}

// load is what the server rendered; a row a queued edit changed (dirty) has
// only an optimistic load, which may differ from the canonical one, so it is
// not sent.
function ref(i) {
  const r = rows[i];
  return { index: i, day: r.day, exercise: r.exercise, set: r.set, ...(r.dirty ? {} : { load: r.load }) };
}

// Offline queue (web queue.js). Every row edit is queued, applied to the
// local rows at once, and sent in order. An edit that cannot reach the server
// stays queued. Replays send the ref the edit was made against; the server
// applies them in the same order, so that is the row it will find there.
// queued mirrors the queue's ops: the store below sees every change.
let queued = [], offline = false, latest = null, reload = false, sent = 0;
const failed = [];

const store = {
  async get(k) {
    queued = (await kv.get(k)) ?? [];
    return queued;
  },
  async set(k, v) {
    queued = v;
    await kv.set(k, v);
    // The op just sent is out of the queue now: show the server's answer for its week.
    if (latest?.tab === week) setView(latest.view);
    latest = null;
    showPending();
  },
};

async function send(op) {
  let res;
  try {
    res = await fetch(API + weekPath(op.tab) + opPath(op), withToken({
      method: opMethod[op.kind],
      headers: { "content-type": "application/json" },
      body: JSON.stringify(op.body),
    }));
  } catch (e) {
    offline = true;
    throw e;
  }
  offline = res.headers.get("X-Offline") === "1";
  if (res.status === 401) await unauthorized();
  if (res.ok && !offline) {
    sent++;
    latest = { tab: op.tab, view: await res.clone().json() };
    keep(rowsPath(op.tab), latest.view);
  }
  return res;
}

// A refused edit (409 stale ref, or any other error) is dropped and reported,
// and its week reloaded; it is never retried against another row.
function drop(op, res) {
  failed.push(res.text().then((t) => describe(op) + (res.status === 409 ? ": the sheet changed meanwhile" : ": " + t.trim())));
  if (op.tab === week) reload = true;
}

const queue = await createQueue({ send, onDrop: drop, store });

function showPending() {
  const p = $("pending");
  p.textContent = queued.length + " pending";
  p.classList.toggle("hidden", !queued.length || !offline);
}

// setView takes a week view from the server: {days, rows, warning}.
function setView(v) {
  base = v.rows;
  days = v.days || [];
  overlay();
}

function overlay() {
  rows = queued.filter((op) => op.tab === week).reduce(applyOp, base);
  markDone();
  fillDays();
  render();
}

const opPath = (op) => ({ patch: "/rows", duplicate: "/rows/duplicate", delete: "/rows/delete" })[op.kind]
  ?? "/days/" + encodeURIComponent(op.day) + "/start";
const opMethod = { patch: "PATCH", duplicate: "POST", delete: "POST", start: "POST" };

function describe(op) {
  if (op.kind === "start") return `${op.tab} ${op.day} (start)`;
  const { day: d, exercise, set } = op.body.ref;
  const what = op.kind === "patch"
    ? Object.entries(op.body.patch).map(([k, v]) => k + " " + JSON.stringify(v)).join(", ")
    : op.kind;
  return `${op.tab} ${d} ${exercise} set ${set} (${what})`;
}

async function edit(kind, body, d) {
  const op = { tab: week, kind, body, ...(d ? { day: d } : {}) };
  if (kind === "duplicate" || kind === "delete") {
    // Re-key done: apply the op to rows that carry their done flag.
    done = new Set(applyOp(rows, op).filter((r) => r.done).map(doneKey));
    saveDone();
  }
  await queue.push(op);
  overlay();
  showPending();
  settle();
}

// settle reports what a flush did once it has ended: dropped edits, a copy
// that is no longer the live state, the offline note.
async function settle() {
  await queue.flush();
  const lost = await Promise.all(failed.splice(0));
  const again = reload;
  reload = false;
  const n = sent;
  sent = 0;
  showPending();
  // Back online after showing a saved copy: fetch the real state.
  if (savedCopy && !offline && navigator.onLine) await refresh(week).catch((e) => status(e.message, true));
  else if (again && week) await load(week).catch((e) => status(e.message, true));
  if (lost.length) status("could not apply " + lost.join("; ") + (again ? "; reloaded the week" : ""), true);
  else if (offline) status("offline: edits are saved on this device and sent when back online");
  else if (n && !savedCopy) status("");
}

const patch = (i, p) => edit("patch", { ref: ref(i), patch: p });

function el(tag, props = {}, ...kids) {
  const e = document.createElement(tag);
  Object.assign(e, props);
  for (const k of kids) if (k != null) e.append(k);
  return e;
}

function input(value, label, onCommit) {
  const i = el("input", { value, spellcheck: false, autocomplete: "off", ariaLabel: label });
  i.setAttribute("autocapitalize", "off");
  i.addEventListener("change", () => onCommit(i.value));
  i.addEventListener("keydown", (e) => { if (e.key === "Enter") i.blur(); });
  return i;
}

function render() {
  setsEl.textContent = "";
  if (!week || day == null) return;
  const mine = rows.filter((r) => r.day === day);
  if (!mine.length) setsEl.append(el("p", { className: "note", textContent: "Nothing planned. Add an exercise to train anyway." }));
  else if (mine.every((r) => r.planned)) {
    setsEl.append(el("p", { className: "note", textContent: `Planned by ${state.block.program}. Plan changes show up here until you log a set, edit, or tick one done; then the day is yours.` }));
  }
  let card = null, prev = null;
  rows.forEach((r, i) => {
    if (r.day !== day) return;
    if (!prev || r.exercise !== prev.exercise) {
      card = el("section", { className: "exercise" + (r.planned ? " planned" : "") }, el("h2", { textContent: r.exercise }));
      setsEl.append(card);
    }
    prev = r;
    const tick = el("button", { className: "done-toggle", ariaLabel: r.done ? "mark not done" : "mark done" });
    tick.setAttribute("aria-pressed", String(r.done));
    tick.addEventListener("click", () => {
      const k = doneKey(r);
      if (done.has(k)) done.delete(k); else done.add(k);
      saveDone();
      // Doing a set starts its day, so the log holds what was trained.
      if (r.planned) edit("start", null, r.day);
      else { markDone(); render(); }
    });
    const field = (f, label) => {
      const x = input(r[f], label, (v) => patch(i, { [f]: v }));
      x.placeholder = label;
      return x;
    };
    // A load that is not one workline set is edited as the raw cell.
    const fields = r.invalid
      ? [Object.assign(field("load", "load, e.g. 1x5 100kg @8 2m"), { className: "load" })]
      : [field("reps", "reps"), field("weight", "weight"), field("rpe", "rpe"), field("rest", "rest")];
    const line = el("div", { className: "set" + (r.done ? " done" : "") + (r.invalid ? " invalid" : "") },
      el("span", { className: "n", textContent: r.set }), ...fields, tick);
    const copy = el("button", { textContent: "copy", ariaLabel: "duplicate set" });
    copy.addEventListener("click", () => edit("duplicate", { ref: ref(i) }));
    const del = el("button", { textContent: "del", className: "danger", ariaLabel: "delete set" });
    del.addEventListener("click", () => {
      if (confirm(`Delete ${r.exercise} set ${r.set}?`)) edit("delete", { ref: ref(i) });
    });
    const notes = input(r.notes, "notes, #tags", (v) => patch(i, { notes: v }));
    notes.placeholder = "notes #tags";
    const meta = el("div", { className: "meta" }, el("div", { className: "meta-row" }, notes, copy, del));
    if (r.tags?.length) meta.append(el("div", { className: "tags" }, ...r.tags.map((t) => el("span", { className: "tag", textContent: "#" + t }))));
    if (r.invalid) meta.append(el("div", { className: "warn", textContent: "not workline: " + r.invalid }));
    card.append(line, meta);
  });
  const add = el("button", { textContent: "+ Add exercise", className: "add-row" });
  add.addEventListener("click", () => addExercise(day));
  setsEl.append(add);
}

// addExercise adds sets to a day from a name and a workline load
// ("3x10 20kg"). It needs the network: the server expands the load.
async function addExercise(d) {
  const exercise = prompt("Exercise:");
  if (!exercise) return;
  const load = prompt(`${exercise}: sets and load (workline, e.g. 3x10 20kg)`);
  if (!load) return;
  if (queue.size()) return status("send the pending edits first (needs the network)", true);
  try {
    status("adding " + exercise);
    const v = await api(weekPath(week) + "/days/" + encodeURIComponent(d) + "/exercises", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ exercise, load }),
    });
    setView(v);
    status("");
  } catch (e) { status(e.offline ? "offline: adding an exercise needs the network" : e.message, true); }
}

weekSel.addEventListener("change", () => { day = null; load(weekSel.value).catch((e) => status(e.message, true)); });
daySel.addEventListener("change", () => { day = daySel.value; render(); });

// Program view: the program sheet's block tabs. Opening one shows its week
// read-only with rows that are not workline marked; Start archives the
// active block and starts this one. Plans are edited in the sheet.
const routineView = $("routine-view");

function showView(v) {
  document.body.classList.toggle("routines", v === "routines");
  $("btn-routines").textContent = v === "routines" ? "Log" : "Program";
  status("");
  if (v === "routines") listPrograms();
  else render();
}

$("btn-routines").addEventListener("click", () => showView(document.body.classList.contains("routines") ? "log" : "routines"));

function listPrograms() {
  routineView.textContent = "";
  const list = el("div", { className: "routine-list" });
  for (const tab of state.programs) {
    const b = el("button", { textContent: tab + (tab === state.block.program ? "  (active)" : "") });
    b.addEventListener("click", () => openProgram(tab));
    list.append(b);
  }
  list.append(el("p", { className: "note", textContent: state.programs.length
    ? "Blocks from the program sheet, one tab each, named by their weeks."
    : "no block tabs in the program sheet (named like w41-46)" }));
  routineView.append(list);
}

async function openProgram(tab) {
  let p;
  try {
    status("loading");
    p = await api("/programs/" + encodeURIComponent(tab));
    status("");
  } catch (e) {
    return status(e.offline ? "offline: the program needs the network" : e.message, true);
  }
  routineView.textContent = "";
  const back = el("button", { textContent: "All blocks" });
  back.addEventListener("click", () => { status(""); listPrograms(); });
  routineView.append(el("div", { className: "editor-head" }, el("h2", { textContent: `${p.tab}, ${p.weeks.length} week${p.weeks.length === 1 ? "" : "s"}` }), back));
  for (const d of p.days) {
    const card = el("section", { className: "exercise rrow" }, el("h2", { textContent: d.name || "(no day)" }));
    if (!d.rows.length) card.append(el("p", { className: "note", textContent: "rest" }));
    for (const r of d.rows) {
      card.append(el("div", { className: "cells" }, el("span", { textContent: r.exercise + "  " }), el("code", { textContent: r.load })));
      if (r.error) card.append(el("div", { className: "err", textContent: r.error }));
    }
    routineView.append(card);
  }
  if (p.errors) {
    routineView.append(el("p", { className: "err", textContent: `${p.errors} row${p.errors === 1 ? " is" : "s are"} not workline; fix ${p.errors === 1 ? "it" : "them"} in the program sheet to start this block.` }));
    return;
  }
  const start = el("button", { textContent: "Start this block", className: "primary" });
  start.addEventListener("click", () => startBlock(p.tab));
  routineView.append(el("div", { className: "bar" }, start));
}

async function startBlock(tab) {
  const b = state.block;
  const msg = b.weeks.length
    ? `Archive ${b.program || "the active block"} (${b.weeks.join(", ")}) and start ${tab}?`
    : `Start ${tab}?`;
  if (!confirm(msg)) return;
  if (queue.size()) return status("send the pending edits first (needs the network)", true);
  try {
    status(b.weeks.length ? "archiving and starting " + tab : "starting " + tab);
    await api("/block", { method: "POST", body: new URLSearchParams({ program: tab }) });
    day = null;
    remember("week", null);
    await refresh();
    showView("log");
  } catch (e) { status(e.offline ? "offline: a new block needs the network" : e.message, true); }
}

$("btn-sheet").addEventListener("click", () => {
  const f = $("sheet");
  if (!f.src) f.src = f.dataset.src;
  f.classList.toggle("hidden");
});

if (!native) register({ version: "1" })?.catch(() => {});

addEventListener("online", settle);
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") settle(); });

(async () => {
  showPending();
  try {
    await refresh();
  } catch (e) {
    status(e.offline ? "offline and no saved copy yet" : e.message, true);
  }
  settle();
})();
