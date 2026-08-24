#!/usr/bin/env bash
# build-android.sh — Build the FreeNet Go DPI engine as an Android AAR.
#
# Prerequisites:
#   - Go 1.21+ (https://go.dev/dl/)
#   - Android NDK (set ANDROID_NDK_HOME or NDK_ROOT)
#   - gomobile (installed automatically by this script)
#
# Usage:
#   cd go-freenet
#   bash scripts/build-android.sh
#
# Output:
#   android/app/libs/mobile.aar        — AAR for the Gradle project
#   android/app/libs/mobile-sources.jar — Java source stubs (optional)
#
# After running this script, open the android/ directory in Android Studio
# and build / run the APK.

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────

JAVA_PKG="com.freenet.bypass"   # Java/Kotlin package for generated bindings
GO_PKG="./internal/mobile"     # Go package to bind
ANDROID_MIN_API=26              # Android 8.0 minimum
OUT_DIR="$(pwd)/android/app/libs"
OUT_AAR="$OUT_DIR/mobile.aar"

TARGETS="android/arm64,android/arm,android/amd64"

# ── Helpers ───────────────────────────────────────────────────────────────────

info()  { echo "[build-android] $*"; }
error() { echo "[build-android] ERROR: $*" >&2; exit 1; }

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || error "Required command not found: $1"
}

# ── Checks ────────────────────────────────────────────────────────────────────

require_cmd go
GO_VERSION=$(go version | grep -oP 'go\d+\.\d+' | head -1)
info "Go version: $GO_VERSION"

# Check Android NDK.
NDK="${ANDROID_NDK_HOME:-${NDK_ROOT:-}}"
if [[ -z "$NDK" ]]; then
    # Try common Android Studio locations.
    for candidate in \
        "$HOME/Android/Sdk/ndk/$(ls "$HOME/Android/Sdk/ndk/" 2>/dev/null | sort -V | tail -1)" \
        "$HOME/Library/Android/sdk/ndk/$(ls "$HOME/Library/Android/sdk/ndk/" 2>/dev/null | sort -V | tail -1)"
    do
        if [[ -d "$candidate" ]]; then
            NDK="$candidate"
            break
        fi
    done
fi

if [[ -z "$NDK" ]]; then
    error "Android NDK not found. Set ANDROID_NDK_HOME or NDK_ROOT environment variable.
Example: export ANDROID_NDK_HOME=\$HOME/Android/Sdk/ndk/26.3.11579264"
fi
info "NDK: $NDK"
export ANDROID_NDK_HOME="$NDK"

# ── Install / update gomobile ─────────────────────────────────────────────────

info "Installing/updating gomobile..."
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest

GOMOBILE="$(go env GOPATH)/bin/gomobile"
[[ -x "$GOMOBILE" ]] || error "gomobile not found after install"

info "Initialising gomobile..."
"$GOMOBILE" init

# ── Build ─────────────────────────────────────────────────────────────────────

info "Building AAR for targets: $TARGETS"
mkdir -p "$OUT_DIR"

"$GOMOBILE" bind \
    -target     "$TARGETS" \
    -androidapi "$ANDROID_MIN_API" \
    -javapkg    "$JAVA_PKG" \
    -o          "$OUT_AAR" \
    -v \
    "$GO_PKG"

info "✓ Built: $OUT_AAR ($(du -h "$OUT_AAR" | cut -f1))"
info ""
info "Next steps:"
info "  1. Open android/ in Android Studio"
info "  2. Build > Make Project"
info "  3. Run on device/emulator"
