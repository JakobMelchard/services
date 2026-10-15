#!/usr/bin/env bash
# Builds the signed release APK of capacitor/android as $GITHUB_WORKSPACE/workouts-$VERSION.apk.
# Env: VERSION (x.y.z), ANDROID_KEYSTORE_B64, ANDROID_KEYSTORE_PASSWORD, optional ANDROID_KEY_ALIAS,
# RUNNER_TEMP, GITHUB_WORKSPACE. Runs from the repo root.
set -euo pipefail

printf '%s' "$ANDROID_KEYSTORE_B64" | base64 -d >"$RUNNER_TEMP/release.jks"
IFS=. read -r major minor patch <<<"$VERSION"
export KEYSTORE_PATH="$RUNNER_TEMP/release.jks"
trap 'rm -f "$KEYSTORE_PATH"' EXIT
# The keystore has no "fz" alias: unless /release sets ANDROID_KEY_ALIAS, sign with its only key.
if [ -z "${ANDROID_KEY_ALIAS:-}" ]; then
  ANDROID_KEY_ALIAS=$(keytool -list -keystore "$KEYSTORE_PATH" -storepass "$ANDROID_KEYSTORE_PASSWORD" |
    awk -F', ' '/PrivateKeyEntry/ { print $1; exit }')
  export ANDROID_KEY_ALIAS
fi
echo "signing with alias: $ANDROID_KEY_ALIAS"
cd capacitor/android
chmod +x gradlew
./gradlew --no-daemon assembleRelease \
  -PversionName="$VERSION" -PversionCode="$((major * 10000 + minor * 100 + patch))"
cp app/build/outputs/apk/release/app-release.apk "$GITHUB_WORKSPACE/workouts-$VERSION.apk"
