// The week's rows and the block are kept by the page (ui.js), not here.

// ServiceWorkerGlobalScope, typed by hand: the webworker lib clashes with dom in one tsconfig.
const sw = /** @type {{ WEB_SW: { cache: string, shell: string[], network: { timeoutMs: number }, offlinePage: string }, importScripts: (...urls: string[]) => void }} */ (/** @type {unknown} */ (self));
sw.WEB_SW = {
  cache: "workouts",
  shell: ["/", "/web/tokens.css", "/web/base.css", "/static/style.css", "/static/ui.js", "/static/icon-192.png", "/static/manifest.webmanifest"],
  network: { timeoutMs: 4000 },
  offlinePage: "/",
};
sw.importScripts("/web/sw.js");
