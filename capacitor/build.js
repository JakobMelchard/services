// Builds www/, the web assets Capacitor bundles: internal/workouts/ui/static as
// is, the shared shell (@jakobmelchard/web, the server's /web/) under www/web,
// plus index.html rendered from the server's template with no embed and with
// the API origin (static/config.js) and the Capacitor runtime (registerPlugin
// for ui.js) loaded first.
// WORKOUTS_API picks the server, e.g. a dev box on the tailnet.
"use strict";
const fs = require("node:fs");
const path = require("node:path");

const api = (process.env.WORKOUTS_API || "https://workouts.lilfeelz.org").replace(/\/+$/, "");
if (!/^https?:\/\/[^/\s]+$/.test(api)) throw new Error(`WORKOUTS_API must be an origin like https://host[:port], got ${api}`);

const ui = path.join(__dirname, "..", "internal", "workouts", "ui");
const mod = (...p) => path.join(__dirname, "node_modules", ...p);
const www = path.join(__dirname, "www");
fs.rmSync(www, { recursive: true, force: true });
fs.cpSync(path.join(ui, "static"), path.join(www, "static"), { recursive: true });
fs.cpSync(mod("@jakobmelchard", "web", "dist"), path.join(www, "web"), { recursive: true });
fs.copyFileSync(mod("@capacitor", "core", "dist", "capacitor.js"), path.join(www, "static", "capacitor.js"));

// handler.Index renders index.html with one value, Embed; the app has none.
let html = fs.readFileSync(path.join(ui, "templates", "index.html"), "utf8")
  .replace("{{if not .Embed}} hidden{{end}}", " hidden")
  .replace('data-src="{{.Embed}}"', 'data-src=""');
if (html.includes("{{")) throw new Error("index.html has a template action build.js does not know: " + html.match(/{{[^}]*}}/)[0]);
const tag = '<script type="module" src="/static/ui.js"></script>';
if (!html.includes(tag)) throw new Error("index.html does not load " + tag);
fs.writeFileSync(path.join(www, "static", "config.js"), `window.WORKOUTS_API = ${JSON.stringify(api)};\n`);
html = html.replace(tag, `<script src="/static/config.js"></script>\n<script src="/static/capacitor.js"></script>\n${tag}`);
fs.writeFileSync(path.join(www, "index.html"), html);
console.log(`www/ built for ${api}`);
