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

func videoStream(display: SCDisplay, excluding ownApp: SCRunningApplication, rect: CGRect,
                 scale: CGFloat, events: StreamEvents, window: SCWindow? = nil) throws -> (SCStream, SCStreamConfiguration) {
    let filter = window.map { SCContentFilter(desktopIndependentWindow: $0) } ?? SCContentFilter(display: display, excludingApplications: [ownApp], exceptingWindows: [])
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
    configuration.capturesAudio = false
    configuration.excludesCurrentProcessAudio = true
    return (SCStream(filter: filter, configuration: configuration, delegate: events), configuration)
}
