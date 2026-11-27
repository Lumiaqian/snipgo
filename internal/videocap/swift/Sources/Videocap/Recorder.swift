import AVFoundation
import ScreenCaptureKit

func makeRecorder() -> any Recorder { RegionRecorder() }

private actor RegionRecorder: Recorder {
    private let samples = DispatchQueue(label: "app.snipgo.video.samples")
    private var active: (SCStream, VideoWriter)?

    func start(path: String, displayID: Int64, x: Int64, y: Int64, width: Int64, height: Int64, scale: Double) async throws {
        let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
        guard active == nil, let id = UInt32(exactly: displayID),
              let display = content.displays.first(where: { $0.displayID == id }),
              let ownApp = content.applications.first(where: { $0.processID == ProcessInfo.processInfo.processIdentifier })
        else { throw CocoaError(.featureUnsupported) }
        let rect = CGRect(x: Int(x), y: Int(y), width: Int(width), height: Int(height))
        guard scale.isFinite, scale > 0, !rect.isEmpty,
              CGRect(origin: .zero, size: display.frame.size).contains(rect)
        else { throw CocoaError(.validationMissingMandatoryProperty) }
        let stream = try videoStream(display: display, excluding: ownApp, rect: rect, scale: scale, systemAudio: false, cursor: true)
        let sink = try VideoWriter(url: URL(fileURLWithPath: path), width: Int((rect.width * scale).rounded()) / 2 * 2,
                                   height: Int((rect.height * scale).rounded()) / 2 * 2, withAudio: false)
        try stream.addStreamOutput(sink, type: .screen, sampleHandlerQueue: samples)
        try await stream.startCapture()
        active = (stream, sink)
    }

    func pause(paused: Bool) async throws {
        guard let (_, sink) = active else { throw CocoaError(.userCancelled) }
        samples.sync { sink.setPaused(paused, at: CMClockGetTime(CMClockGetHostTimeClock())) }
    }

    func stop() async throws {
        guard let (stream, sink) = active else { throw CocoaError(.userCancelled) }
        active = nil
        let end = CMClockGetTime(CMClockGetHostTimeClock())
        // A disconnected display can stop the stream before the user stops recording.
        try? await stream.stopCapture()
        try samples.sync { try sink.finish(at: end) }
    }
}
