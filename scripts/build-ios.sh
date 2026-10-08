#!/bin/bash
# Build libghostty-vt for the selected SDK, then package the native GoRex app.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
GHOSTTY_COMMIT=befcdfd2c3a1cb24d9ec886e93c95b2b5daa7028
ZIG_VERSION=0.16.0
WORK="$ROOT/.mygo/ios"
mkdir -p "$WORK"
if [[ ! -x "$WORK/zig-aarch64-macos-$ZIG_VERSION/zig" ]]; then
 curl -fL "https://ziglang.org/download/$ZIG_VERSION/zig-aarch64-macos-$ZIG_VERSION.tar.xz" -o "$WORK/zig.tar.xz"
 tar -xf "$WORK/zig.tar.xz" -C "$WORK"
fi
if [[ ! -d "$WORK/ghostty/.git" ]]; then
 git clone --filter=blob:none --no-checkout https://github.com/ghostty-org/ghostty.git "$WORK/ghostty"
fi
git -C "$WORK/ghostty" checkout "$GHOSTTY_COMMIT"
TARGET=aarch64-ios
PREFIX="$WORK/ghostty-device"
for arg in "$@"; do
 if [[ "$arg" == -ios-simulator ]]; then TARGET=aarch64-ios-simulator; PREFIX="$WORK/ghostty-simulator"; fi
done
(cd "$WORK/ghostty" && "$WORK/zig-aarch64-macos-$ZIG_VERSION/zig" build -Demit-lib-vt -Dtarget="$TARGET" -Doptimize=ReleaseFast -Demit-xcframework=false --prefix "$PREFIX")
PLATFORM=ios
SDK=iphoneos
if [[ "$TARGET" == aarch64-ios-simulator ]]; then PLATFORM=ios-simulator; SDK=iphonesimulator; fi
SDK_VERSION=$(xcrun --sdk "$SDK" --show-sdk-version)
python3 scripts/generate-ios-vt.py
gofmt -w internal/terminal/internal/vt/open_ios.go
xcrun ld -r -arch arm64 -platform_version "$PLATFORM" 15.0 "$SDK_VERSION" -all_load "$PREFIX/lib/libghostty-vt.a" -o internal/terminal/internal/vt/ghostty_ios_arm64.syso
go tool mygo build -platform ios/arm64 "$@" .
