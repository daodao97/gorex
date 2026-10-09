#!/bin/bash
# Use the reviewed, checksum-pinned App Store Connect CLI for this repository.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) platform=macOS_arm64 ;;
  Darwin/x86_64) platform=macOS_amd64 ;;
  *) echo 'Retty iOS releases require macOS and Xcode.' >&2; exit 1 ;;
esac
read -r version checksum < <(python3 - "$platform" <<'PY'
import json, sys
from pathlib import Path
config = json.loads(Path('.asc/toolchain.json').read_text())
print(config['version'], config['sha256'][sys.argv[1]])
PY
)
TOOL_DIR="$ROOT/.mygo/tools/asc"
mkdir -p "$TOOL_DIR"
binary="$TOOL_DIR/asc"
if [[ ! -x "$binary" ]] || [[ "$(shasum -a 256 "$binary" | cut -d ' ' -f 1)" != "$checksum" ]]; then
  download=$(mktemp "$TOOL_DIR/download-XXXXXX")
  trap 'rm -f "$download"' EXIT
  curl -fL --silent --show-error \
    "https://github.com/rorkai/App-Store-Connect-CLI/releases/download/$version/asc_${version}_${platform}" \
    -o "$download"
  if [[ "$(shasum -a 256 "$download" | cut -d ' ' -f 1)" != "$checksum" ]]; then
    echo "asc $version checksum verification failed." >&2
    exit 1
  fi
  chmod 755 "$download"
  mv -f "$download" "$binary"
fi
export PATH="$TOOL_DIR:$PATH"
export ASC_TELEMETRY_DISABLED=1
exec "$binary" "$@"
