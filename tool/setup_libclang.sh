#!/usr/bin/env bash
set -euo pipefail

if ! compgen -G '/usr/lib/llvm-*/lib/libclang.so*' > /dev/null; then
  sudo apt-get update
  sudo apt-get install -y libclang-dev
fi
libclang="$(find /usr/lib/llvm-* -maxdepth 2 -name 'libclang.so*' -print | sort -V | tail -n 1)"
test -n "$libclang"
echo "LIBCLANG_PATH=$(dirname "$libclang")" >> "${GITHUB_ENV:?}"
