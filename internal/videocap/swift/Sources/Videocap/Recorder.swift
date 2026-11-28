import AVFoundation
import ScreenCaptureKit

actor RegionRecorder {
    private let events = StreamEvents()
    private let samples = DispatchQueue(label: "app.snipgo.video.samples")
    private var configuration: SCStreamConfiguration?
    private var microphone: Microphone?
    private var active: (SCStream, VideoWriter)?

    func start(path: String, displayID: Int64, x: Int64, y: Int64, width: Int64, height: Int64, scale: Double) async throws {
        let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
        guard active == nil, let id = UInt32(exactly: displayID),
              let display = content.displays.first(where: { $0.displayID == id }),
              let ownApp = content.applications.first(where: { $0.processID == getppid() })
        else { throw CocoaError(.featureUnsupported) }
        let rect = CGRect(x: Int(x), y: Int(y), width: Int(width), height: Int(height))
        guard scale.isFinite, scale > 0, !rect.isEmpty,
              CGRect(origin: .zero, size: display.frame.size).contains(rect)
        else { throw CocoaError(.validationMissingMandatoryProperty) }
        let (stream, configuration) = try videoStream(display: display, excluding: ownApp, rect: rect, scale: scale, events: events)
        let sink = try VideoWriter(url: URL(fileURLWithPath: path), width: Int((rect.width * scale).rounded()) / 2 * 2,
                                   height: Int((rect.height * scale).rounded()) / 2 * 2)
        try stream.addStreamOutput(sink, type: .screen, sampleHandlerQueue: samples)
        try stream.addStreamOutput(sink, type: .audio, sampleHandlerQueue: samples)
        try await stream.startCapture()
        self.configuration = configuration
        active = (stream, sink)
    }

    func setSystem(enabled: Bool) async throws {
        guard let (stream, _) = active, let configuration else { throw CocoaError(.userCancelled) }
        let previous = configuration.capturesAudio
        configuration.capturesAudio = enabled
        do { try await stream.updateConfiguration(configuration) }
        catch { configuration.capturesAudio = previous; throw error }
    }

    func setMic(enabled: Bool) async throws {
        if !enabled { microphone?.stop(); microphone = nil; return }
        guard microphone == nil, let (_, sink) = active,
              await AVCaptureDevice.requestAccess(for: .audio) else { throw CocoaError(.userCancelled) }
        let mic = try Microphone(queue: samples) { time, buffer in
            sink.receive(buffer, from: .microphone, at: time)
        }
        mic.session.startRunning()
        guard mic.session.isRunning else { throw CocoaError(.featureUnsupported) }
        microphone = mic
    }

    // Negative means the stream ended independently of the controls.
    func level() async throws -> Double {
        if events.interrupted { return -1 }
        guard let (_, sink) = active else { return 0 }
        return samples.sync { Double(sink.takePeak()) }
    }

    func pause(paused: Bool) async throws {
        guard let (_, sink) = active else { throw CocoaError(.userCancelled) }
        samples.sync { sink.setPaused(paused, at: CMClockGetTime(CMClockGetHostTimeClock())) }
    }

    func stop() async throws {
        guard let (stream, sink) = active else { throw CocoaError(.userCancelled) }
        active = nil
        microphone?.stop()
        microphone = nil
        configuration = nil
        let end = CMClockGetTime(CMClockGetHostTimeClock())
        // A disconnected display can stop the stream before the user stops recording.
        try? await stream.stopCapture()
        try samples.sync { try sink.finish(at: end) }
    }
}
