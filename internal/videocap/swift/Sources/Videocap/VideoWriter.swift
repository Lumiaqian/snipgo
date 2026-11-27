import AVFoundation
import VideoToolbox
import ScreenCaptureKit
import CaptureAudio

// Register this output and submit pause/resume changes on the same serial queue.
final class VideoWriter: NSObject, SCStreamOutput {
    let writer: AVAssetWriter
    private let input: AVAssetWriterInput
    private var pauseStart: CMTime?
    private var removed = CMTime.zero
    private(set) var failure: Error?

    private var peak: Float = 0
    func takePeak() -> Float {
        defer { peak = 0 }
        return peak
    }

    private var audio: AACTrack?
    private var timeline = AudioTimeline()
    private var origin: CMTime?
    private var lastVideo = CMTime.zero

    init(url: URL, width: Int, height: Int, withAudio: Bool) throws {
        writer = try AVAssetWriter(outputURL: url, fileType: .mp4)
        input = AVAssetWriterInput(mediaType: .video, outputSettings: [
            AVVideoCodecKey: AVVideoCodecType.h264,
            AVVideoWidthKey: width, AVVideoHeightKey: height,
            AVVideoEncoderSpecificationKey: [
                kVTVideoEncoderSpecification_RequireHardwareAcceleratedVideoEncoder as String: true
            ]
        ])
        input.expectsMediaDataInRealTime = true
        guard writer.canAdd(input) else { throw CocoaError(.featureUnsupported) }
        writer.add(input)
        if withAudio { audio = try AACTrack(writer: writer) }
    }

    func setPaused(_ paused: Bool, at hostTime: CMTime) {
        if paused {
            if pauseStart == nil { pauseStart = hostTime }
        } else if let start = pauseStart {
            removed = removed + hostTime - start
            pauseStart = nil
        }
    }

    func receive(_ samples: [Float], from source: AudioTimeline.Source, at time: CMTime) {
        guard pauseStart == nil, let origin,
              let frame = framePosition(hostTime: time, origin: origin, paused: removed) else { return }
        for i in stride(from: 0, to: samples.count, by: 16) { peak = max(peak, abs(samples[i])) }
        timeline.insert(samples, from: source, at: frame)
    }

    private func drain(to time: CMTime) throws {
        guard let audio, let origin,
              let end = framePosition(hostTime: time, origin: origin, paused: .zero) else { return }
        while timeline.position < end, audio.input.isReadyForMoreMediaData {
            let start = timeline.position
            let samples = timeline.render(frames: Int(min(1024, end - start)))
            try audio.append(samples, at: origin + CMTime(value: start, timescale: 48_000))
        }
    }

    func finish(at time: CMTime) throws {
        if let failure { throw failure }
        guard writer.status == .writing else { throw writer.error ?? CocoaError(.fileWriteUnknown) }
        let end = max(lastVideo, (pauseStart ?? time) - removed)
        try drain(to: end)
        if audio != nil, let origin,
           let expected = framePosition(hostTime: end, origin: origin, paused: .zero),
           timeline.position < expected { throw CocoaError(.fileWriteUnknown) }
        writer.endSession(atSourceTime: end)
        audio?.input.markAsFinished()
        input.markAsFinished()
        let finished = DispatchSemaphore(value: 0)
        writer.finishWriting { finished.signal() }
        finished.wait()
        guard writer.status == .completed else { throw writer.error ?? CocoaError(.fileWriteUnknown) }
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer,
                of type: SCStreamOutputType) {
        if type == .audio {
            do {
                guard let format = sampleBuffer.formatDescription else { throw CocoaError(.fileReadCorruptFile) }
                let pcmFormat = AVAudioFormat(cmAudioFormatDescription: format)
                guard pcmFormat.commonFormat == .pcmFormatFloat32, pcmFormat.sampleRate == 48_000,
                      pcmFormat.channelCount == 2,
                      let pcm = AVAudioPCMBuffer(pcmFormat: pcmFormat, frameCapacity: AVAudioFrameCount(sampleBuffer.numSamples))
                      else { throw CocoaError(.featureUnsupported) }
                pcm.frameLength = pcm.frameCapacity
                let rc = CMSampleBufferCopyPCMDataIntoAudioBufferList(sampleBuffer, at: 0,
                    frameCount: Int32(pcm.frameLength), into: pcm.mutableAudioBufferList)
                guard rc == noErr, let channels = pcm.floatChannelData else { throw CocoaError(.fileReadCorruptFile) }
                let count = Int(pcm.frameLength)
                let stereo = pcmFormat.isInterleaved ? Array(UnsafeBufferPointer(start: channels[0], count: count*2)) :
                    (0..<count).flatMap { [channels[0][$0], channels[1][$0]] }
                receive(stereo, from: .system, at: sampleBuffer.presentationTimeStamp)
            } catch { failure = error }
            return
        }
        guard type == .screen, pauseStart == nil, failure == nil,
              let attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuffer, createIfNecessary: false)
                as? [[SCStreamFrameInfo: Any]],
              attachments.first?[.status] as? Int == SCFrameStatus.complete.rawValue else { return }
        let pts = sampleBuffer.presentationTimeStamp - removed
        if writer.status == .unknown {
            guard writer.startWriting() else { failure = writer.error ?? CocoaError(.fileWriteUnknown); return }
            origin = pts
            writer.startSession(atSourceTime: pts)
        }
        lastVideo = pts
        do { try drain(to: pts - CMTime(value: 1, timescale: 5)) } catch { failure = error; return }
        guard input.isReadyForMoreMediaData else { return }
        var timing = CMSampleTimingInfo(duration: sampleBuffer.duration, presentationTimeStamp: pts,
                                        decodeTimeStamp: .invalid)
        var copy: CMSampleBuffer?
        let status = CMSampleBufferCreateCopyWithNewTiming(allocator: kCFAllocatorDefault,
            sampleBuffer: sampleBuffer, sampleTimingEntryCount: 1, sampleTimingArray: &timing,
            sampleBufferOut: &copy)
        guard status == noErr, let copy else {
            failure = NSError(domain: NSOSStatusErrorDomain, code: Int(status))
            return
        }
        if !input.append(copy) { failure = writer.error ?? CocoaError(.fileWriteUnknown) }
    }
}
