#!/usr/bin/env bash
# build-release-apk.sh — Full pipeline: Go AAR → Android APK.
#
# Run from the go-freenet/ directory:
#   bash scripts/build-release-apk.sh
#
# Outputs:
#   android/app/build/outputs/apk/release/app-release-unsigned.apk
#   android/app/build/outputs/apk/debug/app-debug.apk
#
# Signing:
#   For a signed release APK create a keystore and set KEYSTORE_PATH,
#   KEY_ALIAS, KEYSTORE_PASSWORD, KEY_PASSWORD environment variables.
#   Otherwise only the debug APK is built.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FREENET_DIR="$(dirname "$SCRIPT_DIR")"

# ── Step 1: Build Go AAR ──────────────────────────────────────────────────────
echo "=== Step 1: Building Go AAR ==="
bash "$SCRIPT_DIR/build-android.sh"

# ── Step 2: Build Android APK ─────────────────────────────────────────────────
echo "=== Step 2: Building Android APK ==="
cd "$FREENET_DIR/android"

if [[ ! -f "gradlew" ]]; then
    # Bootstrap gradlew using the wrapper jar if present.
    if [[ -f "gradle/wrapper/gradle-wrapper.jar" ]]; then
        java -jar gradle/wrapper/gradle-wrapper.jar wrapper
    else
        echo "gradlew not found. Download from Android Studio or run:"
        echo "  gradle wrapper --gradle-version 8.11.1"
        exit 1
    fi
fi

chmod +x ./gradlew

echo "Building debug APK..."
./gradlew assembleDebug

DEBUG_APK="app/build/outputs/apk/debug/app-debug.apk"
if [[ -f "$DEBUG_APK" ]]; then
    echo "✓ Debug APK: $FREENET_DIR/android/$DEBUG_APK ($(du -h "$DEBUG_APK" | cut -f1))"
fi

# Optional: signed release APK.
if [[ -n "${KEYSTORE_PATH:-}" ]]; then
    echo "Building signed release APK..."
    ./gradlew assembleRelease \
        -Pandroid.injected.signing.store.file="$KEYSTORE_PATH" \
        -Pandroid.injected.signing.store.password="${KEYSTORE_PASSWORD:-}" \
        -Pandroid.injected.signing.key.alias="${KEY_ALIAS:-key0}" \
        -Pandroid.injected.signing.key.password="${KEY_PASSWORD:-}"

    RELEASE_APK="app/build/outputs/apk/release/app-release.apk"
    if [[ -f "$RELEASE_APK" ]]; then
        echo "✓ Release APK: $FREENET_DIR/android/$RELEASE_APK"
    fi
fi
