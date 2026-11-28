import Foundation

// Each helper owns exactly one object. Newlines delimit JSON commands and replies;
// pixel data follows its length header verbatim so GIF export needs no base64 copy.
private struct Request: Decodable {
    var method: String
    var path, dst: String?
    var windowID: UInt32?
    var displayID, x, y, width, height, fps: Int64?
    var scale, start, end: Double?
    var enabled: Bool?
}

private struct Reply: Encodable {
    var architecture: String?
    var error: String?
    var dimensions: [Int64]?
    var level: Double?
    var progress: Int64?
    var bytes: Int?
}

private func send(_ reply: Reply, pixels: Data? = nil) throws {
    var line = try JSONEncoder().encode(reply)
    line.append(10)
    try FileHandle.standardOutput.write(contentsOf: line)
    if let pixels { try FileHandle.standardOutput.write(contentsOf: pixels) }
}

private func require<T>(_ value: T?) throws -> T {
    guard let value else { throw CocoaError(.coderValueNotFound) }
    return value
}

private enum Worker {
    case recorder(RegionRecorder)
    case frames(MovieFrames)
    case studio(RecordingStudio)

    func reply(to r: Request) async throws {
        switch (self, r.method) {
        case let (.recorder(o), "start"):
            try await o.start(path: require(r.path), displayID: require(r.displayID),
                              x: require(r.x), y: require(r.y), width: require(r.width),
                              height: require(r.height), scale: require(r.scale), windowID: r.windowID)
        case let (.recorder(o), "system"): try await o.setSystem(enabled: require(r.enabled))
        case let (.recorder(o), "mic"): try await o.setMic(enabled: require(r.enabled))
        case let (.recorder(o), "pause"): try await o.pause(paused: require(r.enabled))
        case let (.recorder(o), "stop"): try await o.stop()
        case let (.recorder(o), "level"): return try await send(Reply(level: o.level()))
        case let (.frames(o), "open"):
            let dims = try await o.open(path: require(r.path), start: require(r.start), end: require(r.end),
                                        width: require(r.width), fps: require(r.fps))
            return try send(Reply(dimensions: dims))
        case let (.frames(o), "next"):
            let pixels = try o.next()
            return try send(Reply(bytes: pixels.count), pixels: pixels)
        case let (.studio(o), "export"):
            try await o.export(src: require(r.path), dst: require(r.dst), start: require(r.start),
                               end: require(r.end), width: require(r.width), fps: require(r.fps),
                               mute: require(r.enabled)) { progress in try? send(Reply(progress: progress)) }
        default: throw CocoaError(.featureUnsupported)
        }
        try send(Reply())
    }
}

@main
enum VideoHelper {
    static func main() async throws {
        let worker: Worker
        switch CommandLine.arguments.dropFirst().first {
        case "recorder": worker = .recorder(RegionRecorder())
        case "frames": worker = .frames(MovieFrames())
        case "studio": worker = .studio(RecordingStudio())
        default: throw CocoaError(.featureUnsupported)
        }
        #if arch(arm64)
        let architecture = "arm64"
        #elseif arch(x86_64)
        let architecture = "amd64"
        #endif
        try send(Reply(architecture: architecture))
        // ScreenCaptureKit and AVFoundation can dispatch to the main queue while idle.
        while let line = await Task.detached(operation: { readLine() }).value {
            do {
                let request = try JSONDecoder().decode(Request.self, from: Data(line.utf8))
                try await worker.reply(to: request)
            } catch {
                try send(Reply(error: error.localizedDescription))
            }
        }
    }
}
