#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$project_root"
npm run build
"$project_root/desktop/sync-frontend.sh"

cd "$project_root/desktop"
if command -v wails >/dev/null 2>&1; then
  wails build -clean -trimpath -platform darwin/universal -o 'Sprite Sprout' -s -skipbindings
else
  # Keep the build reproducible on a fresh machine without a globally
  # installed Wails binary. The module version is pinned in desktop/go.mod.
  go run github.com/wailsapp/wails/v2/cmd/wails@v2.14.0 \
    build -clean -trimpath -platform darwin/universal -o 'Sprite Sprout' -s -skipbindings
fi
