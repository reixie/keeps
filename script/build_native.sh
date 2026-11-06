#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
arch="${1:-$(go env GOARCH)}"
case "$arch" in
  arm64) swift_arch=arm64 ;;
  amd64) swift_arch=x86_64 ;;
  *) echo "Expected arm64 or amd64" >&2; exit 1 ;;
esac
minimum_os="$(/usr/bin/plutil -extract macos.minimumSystemVersion raw -o - mygo.json)"
swift_flags=(-O -module-cache-path "${TMPDIR:-/tmp}/keeps-swift-module-cache" -target "$swift_arch-apple-macosx$minimum_os")
media_sources=(native/media.swift)
playback_sources=(native/playback.swift native/capture_menu.swift native/library_controls.swift native/space_symbol.swift native/tags_popover.swift native/settings.swift)

needs_build() {
  local output="$1" source
  shift
  if [[ ! -f "$output" ]]; then
    return 0
  fi
  for source in script/build_native.sh mygo.json "$@"; do
    if [[ ! -f "$source" || "$source" -nt "$output" ]]; then
      return 0
    fi
  done
  return 1
}

resource_dir="resources/darwin-$arch"
mkdir -p "$resource_dir"
media="$resource_dir/KeepsMedia"
if [[ ! -x "$media" ]] || needs_build "$media" "${media_sources[@]}"; then
  xcrun swiftc "${swift_flags[@]}" -parse-as-library "${media_sources[@]}" -o "$media"
fi
playback="$resource_dir/libKeepsPlayback.dylib"
if needs_build "$playback" "${playback_sources[@]}"; then
  xcrun swiftc "${swift_flags[@]}" -emit-library "${playback_sources[@]}" -o "$playback"
fi
