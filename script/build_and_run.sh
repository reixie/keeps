#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

pkill -x Keeps >/dev/null 2>&1 || true
./script/build_native.sh
go tool mygo build -platform "darwin/$(go env GOARCH)" -skip-dmg
app_bundle="$PWD/build/darwin-$(go env GOARCH)/Keeps.app"
launch_options=()
if [[ -n "${KEEPS_LIBRARY:-}" ]]; then
  launch_options+=(--env "KEEPS_LIBRARY=$KEEPS_LIBRARY")
fi
if (( ${#launch_options[@]} )); then
  /usr/bin/open -n "${launch_options[@]}" "$app_bundle"
else
  /usr/bin/open -n "$app_bundle"
fi
