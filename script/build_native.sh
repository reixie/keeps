#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
arch="${1:-$(go env GOARCH)}"
case "$arch" in
  arm64) swift_arch=arm64 ;;
  amd64) swift_arch=x86_64 ;;
  *) echo "Expected arm64 or amd64" >&2; exit 1 ;;
esac
destination="resources/darwin-$arch/KeepsMedia"
mkdir -p "$(dirname "$destination")"
if [[ ! -x "$destination" || native/media.swift -nt "$destination" ]]; then
  xcrun swiftc -O -parse-as-library -module-cache-path "${TMPDIR:-/tmp}/keeps-swift-module-cache" -target "$swift_arch-apple-macosx13.0" native/media.swift -o "$destination"
fi
playback="resources/darwin-$arch/libKeepsPlayback.dylib"
if [[ ! -f "$playback" || native/playback.swift -nt "$playback" ]]; then
  xcrun swiftc -O -emit-library -module-cache-path "${TMPDIR:-/tmp}/keeps-swift-module-cache" -target "$swift_arch-apple-macosx13.0" native/playback.swift -o "$playback"
fi
