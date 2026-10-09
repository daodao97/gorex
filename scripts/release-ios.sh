#!/bin/bash
# Canonical iOS release entrypoint. Artifacts always use build/app-store/ios-arm64.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
exec python3 scripts/ios-release.py run "$@"
