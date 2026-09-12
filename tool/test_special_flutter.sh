#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
config_backup="$(mktemp)"
cp pubspec.yaml "$config_backup"
trap 'cp "$config_backup" pubspec.yaml; rm -f "$config_backup"' EXIT

# Tests mock native APIs; packaging must retain both real native build hooks.
test "$(grep -c 'build_assets: true' pubspec.yaml)" -eq 2
sed -i 's/build_assets: true/build_assets: false/g' pubspec.yaml
flutter test --reporter expanded "$@"
