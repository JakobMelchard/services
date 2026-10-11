// applyOp and renumber from static/ops.mjs, which mirror block.Apply,
// block.Duplicate, block.Delete and block.Renumber (internal/workouts/block).
// Run: node --test internal/workouts/ui/test/
import { test } from "node:test";
import assert from "node:assert/strict";

import { applyOp, renumber } from "../static/ops.mjs";

const row = (day, exercise, set, more = {}) => ({ day, exercise, set, load: "1x5 100kg", reps: "5", weight: "100kg", rpe: "", rest: "", notes: "", tags: [], ...more });
const week = () => [row("MON", "Squat", "1"), row("MON", "Squat", "2"), row("MON", "Curl", "1"), row("TUE", "Run", "1", { planned: true })];
const ref = (rs, i) => ({ index: i, day: rs[i].day, exercise: rs[i].exercise, set: rs[i].set });
const sets = (rs) => rs.map((r) => `${r.day} ${r.exercise} ${r.set}`);

test("renumber counts sets per run of one exercise on one day", () => {
  const rs = renumber([row("MON", "Squat", "9"), row("MON", "Squat", "9"), row("MON", "Curl", "9"), row("MON", "Squat", "9"), row("TUE", "Squat", "9")]);
  assert.deepEqual(rs.map((r) => r.set), ["1", "2", "1", "1", "1"]);
});

test("patch sets fields, trims them, clears invalid and derives tags", () => {
  const rs = week();
  rs[1].invalid = "not workline";
  const out = applyOp(rs, { kind: "patch", body: { ref: ref(rs, 1), patch: { weight: " 105kg ", notes: "felt #Easy and #fast" } } });
  assert.equal(out[1].weight, "105kg");
  assert.equal(out[1].invalid, undefined);
  assert.deepEqual([...out[1].tags], ["easy", "fast"]);
  assert.equal(rs[1].weight, "100kg", "input rows unchanged");
});

test("duplicate inserts a copy after the set without notes and renumbers", () => {
  const rs = week();
  rs[0].notes = "deep";
  rs[0].done = true;
  const out = applyOp(rs, { kind: "duplicate", body: { ref: ref(rs, 0) } });
  assert.deepEqual(sets(out), ["MON Squat 1", "MON Squat 2", "MON Squat 3", "MON Curl 1", "TUE Run 1"]);
  assert.equal(out[1].notes, "");
  assert.equal(out[1].done, false);
});

test("delete removes the set and renumbers", () => {
  const rs = week();
  assert.deepEqual(sets(applyOp(rs, { kind: "delete", body: { ref: ref(rs, 0) } })), ["MON Squat 1", "MON Curl 1", "TUE Run 1"]);
});

test("an edit starts its day only", () => {
  const rs = week().map((r) => ({ ...r, planned: true }));
  const out = applyOp(rs, { kind: "delete", body: { ref: ref(rs, 2) } });
  assert.deepEqual(out.map((r) => !!r.planned), [false, false, true]);
  assert.deepEqual(applyOp(rs, { kind: "start", day: "TUE" }).map((r) => !!r.planned), [true, true, true, false]);
});

test("a stale ref leaves the rows alone, like block.ErrStale", () => {
  const rs = week();
  const stale = { ...ref(rs, 0), exercise: "Bench" };
  assert.equal(applyOp(rs, { kind: "delete", body: { ref: stale } }), rs);
  assert.equal(applyOp(rs, { kind: "patch", body: { ref: { ...ref(rs, 0), index: 9 }, patch: { reps: "3" } } }), rs);
});

test("a patched row is dirty, so its ref load is not trusted; notes and copies follow", () => {
  const rs = week();
  const note = applyOp(rs, { kind: "patch", body: { ref: ref(rs, 0), patch: { notes: "x" } } });
  assert.equal(note[0].dirty, undefined);
  const out = applyOp(rs, { kind: "patch", body: { ref: ref(rs, 0), patch: { reps: "3" } } });
  assert.equal(out[0].dirty, true);
  assert.equal(applyOp(out, { kind: "duplicate", body: { ref: ref(out, 0) } })[1].dirty, true);
});
