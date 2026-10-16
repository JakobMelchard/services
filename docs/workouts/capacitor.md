# Phone (Capacitor)

`capacitor/` bundles the workouts UI as native Android and iOS apps; only the API is remote (`WORKOUTS_API`, sent through Capacitor's native HTTP). The app has no login cookie: on its first 401 it asks for the token (`WORKOUTS_TOKEN`), keeps it in Capacitor Preferences and sends it as `Authorization: Bearer` on every request. Clear the app's data to enter another one.

```sh
cd capacitor
npm ci
npm run build                 # www/ from internal/workouts/ui/static, @jakobmelchard/web/dist, index.html
npx cap sync android          # or ios
npx cap open android          # or ios
WORKOUTS_API=http://mimi:8093 npm run build   # default API is https://workouts.lilfeelz.org
```

The npm `@jakobmelchard/web` pin must match the module version in `go.mod`.

| Platform | Install |
|---|---|
| Android | Obtainium on this repo's GitHub releases, APK filter `workouts-.*\.apk` |
| iOS | Run from Xcode on the phone, or sideload the archive with SideStore |

## CI and release

| Workflow | Does |
|---|---|
| `ci.yml` | On every pull request and push to main: debug APK (`android`, uploaded as the `app-debug` artifact) |
| `release.yml` | release-please; the `apk` job builds `assembleRelease` and uploads `workouts-<version>.apk` |

`versionCode` is major*10000 + minor*100 + patch. The keystore comes from Infisical `gha` `/release` (`ANDROID_KEYSTORE_B64`, `ANDROID_KEYSTORE_PASSWORD`, optional `ANDROID_KEY_ALIAS`, else the keystore's only key; the key password must equal the keystore password) by OIDC as `gha-org`; the `apk` job fails until that folder is filled.
