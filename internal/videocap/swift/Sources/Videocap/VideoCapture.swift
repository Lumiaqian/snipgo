import AVFoundation
import ScreenCaptureKit

final class StreamEvents: NSObject, SCStreamDelegate {
    private let lock = NSLock()
    private var ended = false
    func stream(_ stream: SCStream, didStopWithError error: Error) {
        lock.withLock { ended = true }
    }
    var interrupted: Bool { lock.withLock { ended } }
}

enum WindowAudioError: LocalizedError {
    case unverified
    var errorDescription: String? { "无法确认目标窗口所属应用的音频隔离，录制未开始。" }
}

func verifyWindowAudio(owner: pid_t?, windowID: CGWindowID?, includedWindows: [CGWindowID], includedApps: [pid_t]) throws {
    guard let owner, owner > 0, let windowID, includedWindows == [windowID],
          Set(includedApps) == [owner] else { throw WindowAudioError.unverified }
}

func videoStream(display: SCDisplay, excluding ownApp: SCRunningApplication, rect: CGRect,
                 scale: CGFloat, events: StreamEvents, window: SCWindow? = nil, appAudio: Bool = false) throws -> (SCStream, SCStreamConfiguration) {
    let filter = window.map { SCContentFilter(desktopIndependentWindow: $0) } ?? SCContentFilter(display: display, excludingApplications: [ownApp], exceptingWindows: [])
    if appAudio {
        guard #available(macOS 15.2, *), let window, filter.style == .window else { throw WindowAudioError.unverified }
        try verifyWindowAudio(owner: window.owningApplication?.processID, windowID: window.windowID,
                              includedWindows: filter.includedWindows.map(\.windowID),
                              includedApps: filter.includedApplications.map(\.processID))
    }
    let configuration = SCStreamConfiguration()
    if window == nil { configuration.sourceRect = rect }
    configuration.width = Int((rect.width * scale).rounded()) / 2 * 2
    configuration.height = Int((rect.height * scale).rounded()) / 2 * 2
    guard configuration.width >= 2, configuration.height >= 2 else {
        throw CocoaError(.validationMissingMandatoryProperty)
    }
    configuration.minimumFrameInterval = CMTime(value: 1, timescale: 60)
    configuration.showsCursor = true
    configuration.sampleRate = 48_000
    configuration.channelCount = 2
    configuration.capturesAudio = appAudio
    configuration.excludesCurrentProcessAudio = true
    return (SCStream(filter: filter, configuration: configuration, delegate: events), configuration)
}
