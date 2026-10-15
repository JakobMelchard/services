// Every list page is server-rendered, so only the static shell is cached and
// a navigation offline gets the plain note in offline.html.

// ServiceWorkerGlobalScope, typed by hand: the webworker lib clashes with dom in one tsconfig.
const sw =
  /** @type {{ WEB_SW: { cache: string, shell: string[], network: { timeoutMs: number }, offlinePage: string }, importScripts: (...urls: string[]) => void }} */ (
    /** @type {unknown} */ (self)
  )
sw.WEB_SW = {
  cache: 'lists',
  shell: [
    '/web/tokens.css',
    '/web/base.css',
    '/static/style.css',
    '/static/htmx.min.js',
    '/static/icon-192.png',
    '/static/manifest.webmanifest',
    '/static/offline.html',
  ],
  network: { timeoutMs: 4000 },
  offlinePage: '/static/offline.html',
}
sw.importScripts('/web/sw.js')
