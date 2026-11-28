#!/bin/bash
# Run on an isolated macOS GUI host with the matching app open and Agent IPC enabled.
# Only the synthetic window created below is passed to the product CLI.
set -euo pipefail
cli=${1:?pass the packaged SnipGo executable}
work=$(mktemp -d)
fixture=
cleanup() {
  if [[ -n "$fixture" ]]; then kill "$fixture" 2>/dev/null || true; wait "$fixture" 2>/dev/null || true; fi
  rm -rf "$work"
}
trap cleanup EXIT
cat > "$work/fixture.swift" <<'SWIFT'
import AppKit
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let window = NSWindow(contentRect: NSRect(x: 100, y: 100, width: 320, height: 200),
                      styleMask: [.titled], backing: .buffered, defer: false)
window.title = "SnipGo recorder regression"
window.backgroundColor = .systemTeal
window.makeKeyAndOrderFront(nil)
print(window.windowNumber)
fflush(stdout)
app.run()
SWIFT
swiftc "$work/fixture.swift" -o "$work/fixture"
"$work/fixture" > "$work/window" &
fixture=$!
for ((i=0; i<100; i++)); do
  [[ -s "$work/window" ]] && break
  kill -0 "$fixture"
  sleep 0.1
done
read -r window < "$work/window"
[[ "$window" =~ ^[1-9][0-9]*$ ]]
"$cli" record --window "$window" --duration 2 --out "$work/clip.mp4" --json
swift - "$work/clip.mp4" <<'SWIFT'
import AVFoundation
let asset = AVURLAsset(url: URL(fileURLWithPath: CommandLine.arguments[1]))
let generator = AVAssetImageGenerator(asset: asset)
let frame = try generator.copyCGImage(at: CMTime(value: 1, timescale: 1), actualTime: nil)
guard frame.width > 0, frame.height > 0 else { exit(1) }
SWIFT
