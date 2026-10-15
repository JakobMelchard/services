// Usage: node scripts/lists/screenshots.js <base url> <out dir>; run via scripts/lists/screenshots.sh.
// Urgency ages from the fixtures' dates, so the colours depend on the day this runs.
import { chromium, devices } from "playwright";

const [url, out] = process.argv.slice(2);
const b = await chromium.launch();
// A touch phone: the row actions are always shown there (hover:none), so the shots match the device.
const ctx = await b.newContext({ ...devices["iPhone 13"], viewport: { width: 390, height: 844 }, colorScheme: "dark", serviceWorkers: "block" });
const p = await ctx.newPage();
for (const name of ["groceries", "todo"]) {
  await p.goto(`${url}?list=${name}`, { waitUntil: "networkidle" });
  await p.evaluate(() => { /** @type {HTMLElement | null} */ (document.activeElement)?.blur(); scrollTo(0, 0); });
  await p.mouse.move(0, 0);
  await p.screenshot({ path: `${out}/${name}.png` });
}
await b.close();
