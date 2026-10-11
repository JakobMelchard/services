// The week's rows and the block are kept by the page (ui.js), not here.
self.WEB_SW = {
  cache: "workouts",
  shell: ["/", "/web/tokens.css", "/web/base.css", "/static/style.css", "/static/ui.js", "/static/icon-192.png", "/static/manifest.webmanifest"],
  network: { timeoutMs: 4000 },
  offlinePage: "/",
};
importScripts("/web/sw.js");
