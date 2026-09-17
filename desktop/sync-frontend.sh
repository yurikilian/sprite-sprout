#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source_dist="$project_root/dist"
embed_dist="$project_root/desktop/frontend/dist"

if [[ ! -f "$source_dist/index.html" ]]; then
  printf 'Frontend build not found at %s. Run npm run build first.\n' "$source_dist" >&2
  exit 1
fi

mkdir -p "$embed_dist"
# Keep the tracked placeholder so `go test` remains valid before a build.
find "$embed_dist" -mindepth 1 ! -name '.gitkeep' -exec rm -rf -- {} +
cp -R "$source_dist/." "$embed_dist/"
printf 'Synced frontend assets to %s\n' "$embed_dist"
