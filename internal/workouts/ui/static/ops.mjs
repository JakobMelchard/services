// Client-side mirrors of block.Apply, block.Duplicate, block.Delete and block.Renumber.

/**
 * A set as GET /weeks/{w}/rows sends it (block.Row), plus the page's own
 * flags: done (ticked on this device) and dirty (a queued edit changed it).
 * @typedef {{
 *   day: string, exercise: string, set: string, load: string,
 *   reps: string, weight: string, rpe: string, rest: string, notes: string,
 *   tags?: string[], invalid?: string, planned?: boolean,
 *   done?: boolean, dirty?: boolean,
 * }} Row
 */
/**
 * Names the row an edit was made against (block.Ref).
 * @typedef {{ index: number, day: string, exercise: string, set: string, load?: string }} Ref
 */
/** @typedef {{ load?: string, reps?: string, weight?: string, rpe?: string, rest?: string, notes?: string }} Patch */
/**
 * @typedef {{ kind: "start", day: string, body: null }
 *   | { kind: "patch", body: { ref: Ref, patch: Patch } }
 *   | { kind: "duplicate" | "delete", body: { ref: Ref } }} Op
 */

/**
 * renumber mirrors block.Renumber: sets 1..n per run of one exercise on one day.
 * @param {Row[]} rs the week's rows
 * @returns {Row[]} the rows with their sets renumbered
 */
export function renumber(rs) {
  let n = 0
  return rs.map((r, i) => {
    n = i > 0 && r.day === rs[i - 1].day && r.exercise === rs[i - 1].exercise ? n + 1 : 1
    return { ...r, set: String(n) }
  })
}

const tagsOf = (/** @type {string} */ notes) =>
  [...(notes || '').matchAll(/#([\p{L}\p{N}_-]+)/gu)].map((m) => m[1].toLowerCase())
const FIELDS = /** @type {const} */ (['reps', 'weight', 'rpe', 'rest'])

/**
 * applyOp mirrors block.Apply/Duplicate/Delete without the workline
 * canonicalization; the server's answer replaces it once the edit is sent.
 * @param {Row[]} rs the week's rows
 * @param {Op} op the edit
 * @returns {Row[]} the edited rows, rs itself when the ref is stale
 */
export function applyOp(rs, op) {
  // Any edit starts its day: its sets stop being planned.
  const start = (/** @type {Row[]} */ xs, /** @type {string} */ d) =>
    xs.map((r) => (r.day === d && r.planned ? { ...r, planned: false } : r))
  if (op.kind === 'start') return start(rs, op.day)
  const { index: i, day: d, exercise, set } = op.body.ref
  if (!rs[i] || rs[i].day !== d || rs[i].exercise !== exercise || rs[i].set !== set) return rs
  const out = start(rs, d)
  if (op.kind === 'patch') {
    const p = op.body.patch,
      r = { ...rs[i], ...p }
    if (FIELDS.some((f) => p[f] != null) || p.load != null) r.dirty = true
    for (const f of FIELDS) {
      const v = p[f]
      if (v != null) {
        r[f] = v.trim()
        delete r.invalid
      }
    }
    if (p.load != null) {
      r.load = p.load.trim()
      delete r.invalid
    }
    if (p.notes != null) r.tags = tagsOf(p.notes)
    out[i] = r
    return out
  }
  if (op.kind === 'duplicate') out.splice(i + 1, 0, { ...rs[i], done: false, notes: '', tags: [] })
  else out.splice(i, 1)
  return renumber(out)
}
