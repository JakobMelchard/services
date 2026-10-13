# Phone (Capacitor)

`capacitor/` wraps the workouts UI in native Android and iOS apps
(`io.jakobmelchard.workouts`). The assets are bundled, only the API is remote:
`ui.js` prefixes every request with `WORKOUTS_API`, sent through Capacitor's
native HTTP (no CORS), keeps the offline queue in Capacitor Preferences and
registers no service worker. On the web `WORKOUTS_API` is unset and nothing
changes.

```sh
cd capacitor
npm ci
npm run build                 # www/: internal/workouts/ui/static, @jakobmelchard/web/dist, index.html
npx cap sync android          # or ios; copies www/ into the native project
npx cap open android          # or ios: Android Studio / Xcode
```

`npm run build` renders `index.html` from the server's template with the API
origin baked in, default `https://workouts.lilfeelz.org`. For a dev server:
`WORKOUTS_API=http://mimi:8093 npm run build`. The npm `@jakobmelchard/web`
pin must match the module version in `go.mod`.

A 401 sends the app to the server's login page, like the web. The app has no
own token prompt.

## Install

Android: Obtainium, source this repo's GitHub releases
(`https://github.com/JakobMelchard/services`), APK filter `workouts-.*\.apk`.
iOS: build and run from Xcode on the phone, or sideload the archive with
SideStore; nothing is signed for distribution.

## CI and release

`ci.yml` builds a debug APK (`android`, shared `android.yml`) and a simulator
build on mimi (`ios`, shared `xcode.yml`, skipped for bot actors).
`release.yml` runs release-please; on a release the `apk` job builds
`assembleRelease` with the version from the tag (`versionCode` =
major*10000 + minor*100 + patch), signed with the keystore that also signs fz,
and uploads `workouts-<version>.apk` to the release.

The keystore comes from Infisical, project `gha`, folder `/release`
(`ANDROID_KEYSTORE_B64`, `ANDROID_KEYSTORE_PASSWORD`, optional
`ANDROID_KEY_ALIAS`, default `fz`), fetched by OIDC as `gha-org`. Filling
that folder is an owner step that has not run yet: the first release's `apk`
job fails until it has.
