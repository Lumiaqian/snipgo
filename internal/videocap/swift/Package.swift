// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Videocap", platforms: [.macOS(.v14)],
    products: [.executable(name: "SnipGoVideo", targets: ["Videocap"])],
    dependencies: [.package(path: "../../recordaudio")],
    targets: [.executableTarget(name: "Videocap", dependencies: [.product(name: "CaptureAudio", package: "recordaudio")],
                      swiftSettings: [.swiftLanguageMode(.v5), .enableExperimentalFeature("StrictConcurrency")]),
              .testTarget(name: "VideocapTests", dependencies: ["Videocap"])]
)
