import AVFoundation
import OSLog

/// All sample access stays on the supplied serial queue. The receiver owns the copied PCM.
final class Microphone: NSObject, AVCaptureAudioDataOutputSampleBufferDelegate {
    private let logger = Logger(subsystem: "app.snipgo", category: "microphone")
    private let queue: DispatchQueue
    private let deviceName: String
    private var received = 0
    private var discarded = 0
    private var peak: Float = 0
    let session = AVCaptureSession()
    private let output = AVCaptureAudioDataOutput()
    private let receive: (CMTime, [Float]) -> Void

    init(queue: DispatchQueue, receive: @escaping (CMTime, [Float]) -> Void) throws {
        guard AVCaptureDevice.authorizationStatus(for: .audio) == .authorized,
              let device = AVCaptureDevice.default(for: .audio) else {
            throw CocoaError(.userCancelled)
        }
        self.deviceName = device.localizedName
        self.queue = queue
        self.receive = receive
        super.init()
        let input = try AVCaptureDeviceInput(device: device)
        guard session.canAddInput(input) else { throw CocoaError(.featureUnsupported) }
        session.addInput(input)
        output.audioSettings = [
            AVFormatIDKey: kAudioFormatLinearPCM, AVSampleRateKey: 48_000,
            AVNumberOfChannelsKey: 2, AVLinearPCMBitDepthKey: 32,
            AVLinearPCMIsFloatKey: true, AVLinearPCMIsNonInterleaved: false
        ]
        guard session.canAddOutput(output) else { throw CocoaError(.featureUnsupported) }
        session.addOutput(output)
        output.setSampleBufferDelegate(self, queue: queue)
    }

    func captureOutput(_ output: AVCaptureOutput, didOutput sample: CMSampleBuffer,
                       from connection: AVCaptureConnection) {
        received += 1
        let format = sample.formatDescription?.audioStreamBasicDescription
        func discard(_ reason: String) {
            discarded += 1
            if discarded == 1 {
                logger.error("First discarded microphone block: \(reason, privacy: .public), format=\(String(describing: format), privacy: .public)")
            }
        }
        guard let clock = session.synchronizationClock else { return discard("missing session clock") }
        guard let format, format.mSampleRate == 48_000, format.mChannelsPerFrame == 2,
              format.mBytesPerFrame == 8, format.mFormatID == kAudioFormatLinearPCM,
              format.mFormatFlags & kAudioFormatFlagIsFloat != 0 else { return discard("unexpected PCM format") }
        guard let buffer = sample.dataBuffer else { return discard("missing data buffer") }
        guard sample.numSamples > 0 else { return discard("empty block") }
        let pts = CMSyncConvertTime(sample.presentationTimeStamp, from: clock, to: CMClockGetHostTimeClock())
        var samples = [Float](repeating: 0, count: sample.numSamples * 2)
        let status = samples.withUnsafeMutableBytes { bytes in
            CMBlockBufferCopyDataBytes(buffer, atOffset: 0, dataLength: bytes.count, destination: bytes.baseAddress!)
        }
        guard status == noErr else { return discard("PCM copy status \(status)") }
        for value in samples { peak = max(peak, abs(value)) }
        receive(pts, samples)
    }

    func stop() {
        session.stopRunning()
        queue.sync {
            let level = received > discarded ? String(peak) : "unavailable"
            logger.notice("device=\(self.deviceName, privacy: .public) received=\(self.received) discarded=\(self.discarded) rawPeakLinear=\(level, privacy: .public)")
        }
    }
}
