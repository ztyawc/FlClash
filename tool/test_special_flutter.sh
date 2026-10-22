#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
config_backup="$(mktemp)"
cp pubspec.yaml "$config_backup"
trap 'cp "$config_backup" pubspec.yaml; rm -f "$config_backup"' EXIT

test "$(grep -c 'build_assets: true' pubspec.yaml)" -eq 2
sed -i '/^    setup:$/,/^    rust_api:$/s/build_assets: true/build_assets: false/' pubspec.yaml
dart run tool/test.dart -- --reporter expanded "$@"
