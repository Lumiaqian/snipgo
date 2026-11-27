// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Videocap", platforms: [.macOS(.v14)],
    products: [.library(name: "Videocap", type: .dynamic, targets: ["Videocap"])],
    dependencies: [.package(path: "../../recordaudio")],
    targets: [.target(name: "Videocap", dependencies: [.product(name: "CaptureAudio", package: "recordaudio")],
                      swiftSettings: [.swiftLanguageMode(.v5), .enableExperimentalFeature("StrictConcurrency")]),
              .testTarget(name: "VideocapTests", dependencies: ["Videocap"])]
)
