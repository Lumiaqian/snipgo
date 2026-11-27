// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "CaptureAudio",
    platforms: [.macOS(.v14)],
    products: [.library(name: "CaptureAudio", targets: ["CaptureAudio"])],
    targets: [.target(name: "CaptureAudio"), .testTarget(name: "CaptureAudioTests", dependencies: ["CaptureAudio"])]
)
