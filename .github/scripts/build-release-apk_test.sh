#!/usr/bin/env bash
# Runs build-release-apk.sh against a fake keytool and gradlew: alias pick, version code, APK copy.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/build-release-apk.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/repo/capacitor/android" "$tmp/out"
cat >"$tmp/bin/keytool" <<'FAKE'
#!/bin/sh
printf 'Keystore type: PKCS12\n\nupload, Jan 1, 2026, PrivateKeyEntry,\n'
FAKE
cat >"$tmp/repo/capacitor/android/gradlew" <<'FAKE'
#!/bin/sh
printf '%s\n' "$KEYSTORE_PATH $ANDROID_KEY_ALIAS $*" >"$GITHUB_WORKSPACE/gradle-args"
mkdir -p app/build/outputs/apk/release
echo apk >app/build/outputs/apk/release/app-release.apk
FAKE
chmod +x "$tmp/bin/keytool"

run() {
  (cd "$tmp/repo" && PATH="$tmp/bin:$PATH" VERSION=1.2.3 RUNNER_TEMP="$tmp" GITHUB_WORKSPACE="$tmp/out" \
    ANDROID_KEYSTORE_B64=$(printf ks | base64) ANDROID_KEYSTORE_PASSWORD=pw "$@" "$script" >/dev/null)
}
fail() { echo "FAIL: $*" >&2; exit 1; }

run env
want="$tmp/release.jks upload --no-daemon assembleRelease -PversionName=1.2.3 -PversionCode=10203"
[ "$(cat "$tmp/out/gradle-args")" = "$want" ] || fail "gradle args: $(cat "$tmp/out/gradle-args")"
[ "$(cat "$tmp/out/workouts-1.2.3.apk")" = apk ] || fail "apk not copied"
[ ! -e "$tmp/release.jks" ] || fail "keystore left behind"

run env ANDROID_KEY_ALIAS=fz
case $(cat "$tmp/out/gradle-args") in *" fz "*) ;; *) fail "ANDROID_KEY_ALIAS not used" ;; esac
echo ok
