#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
package=internal/videocap/swift
for arch in "$@"; do
  case "$arch" in
    arm64) goarch=arm64 ;;
    x86_64) goarch=amd64 ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
  scratch="build/video-helper-$arch"
  swift build --package-path "$package" --scratch-path "$scratch" --arch "$arch" -c release --product SnipGoVideo
  bin=$(swift build --package-path "$package" --scratch-path "$scratch" --arch "$arch" -c release --show-bin-path)
  dest="resources/darwin-$goarch/bin"
  mkdir -p "$dest"
  install -m 755 "$bin/SnipGoVideo" "$dest/SnipGoVideo"
done
