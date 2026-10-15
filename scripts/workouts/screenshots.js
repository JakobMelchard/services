// Usage: node scripts/workouts/screenshots.js <base url> <out dir>; run via scripts/workouts/screenshots.sh.
// Week w40 is pinned via localStorage so the shots do not depend on today's ISO week.
// Done ticks are app-only (localStorage): everything before Thursday's last Barbell Row set.
import { chromium } from 'playwright'

/** @typedef {import("../../internal/workouts/ui/static/ops.mjs").Row} Row */

const [url, out] = process.argv.slice(2)
const { rows } = /** @type {{ rows: Row[] }} */ (await (await fetch(url + 'weeks/w40/rows')).json())
const done = rows
  .filter(
    (r) =>
      !r.planned &&
      (!r.day.startsWith('THU') ||
        r.exercise === 'Bench Press' ||
        (r.exercise === 'Barbell Row' && r.set !== '3')),
  )
  .map((r) => r.day + '\u0000' + r.exercise + '\u0000' + r.set)
const b = await chromium.launch()
const ctx = await b.newContext({
  viewport: { width: 390, height: 844 },
  deviceScaleFactor: 2,
  colorScheme: 'dark',
  serviceWorkers: 'block',
})
await ctx.addInitScript((d) => {
  localStorage.setItem('week', 'w40')
  localStorage.setItem('done:w38-43:2026-09-14:w40', d)
}, JSON.stringify(done))
const p = await ctx.newPage()
const shot = async (/** @type {string} */ name) => {
  await p.evaluate(() => document.fonts.ready)
  await p.waitForTimeout(400)
  await p.screenshot({ path: `${out}/${name}.png` })
}
await p.goto(url, { waitUntil: 'networkidle' })
await shot('log')
await p.selectOption('#day-select', 'SATURDAY (DEADLIFT)')
await shot('planned')
await p.click('#btn-routines')
await p.waitForTimeout(200)
await p.locator('#routine-view button').first().click()
await shot('program')
await b.close()
