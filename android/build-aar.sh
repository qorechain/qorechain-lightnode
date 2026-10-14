#!/usr/bin/env bash
# Rebuild libs/qorelightnode.aar from the Go light node in ../mobile.
#
# The app does not reimplement the light node: it binds the same Go code the
# server and desktop builds run. Rebuild this whenever ../internal or ../mobile
# changes, or the app ships a stale node.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"

: "${ANDROID_HOME:=/opt/homebrew/share/android-commandlinetools}"
if [ -z "${ANDROID_NDK_HOME:-}" ]; then
  ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1)"
fi
export ANDROID_HOME ANDROID_NDK_HOME
export PATH="$PATH:$(go env GOPATH)/bin"

command -v gomobile >/dev/null || go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init

cd "$here/../mobile"
# arm64 only: every current Android phone. Add android/amd64 here (and to
# app/build.gradle.kts abiFilters) to run on an x86_64 emulator.
gomobile bind -target=android/arm64 -androidapi 26 -o "$here/libs/qorelightnode.aar" .
echo "built $here/libs/qorelightnode.aar"
