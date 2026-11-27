import AVFoundation

/// Accepts the mixer's 48 kHz interleaved stereo output. PTS uses the video writer's timeline.
public final class AACTrack {
    public let input: AVAssetWriterInput
    private let format: CMAudioFormatDescription

    public init(writer: AVAssetWriter) throws {
        var asbd = AudioStreamBasicDescription(mSampleRate: 48_000, mFormatID: kAudioFormatLinearPCM,
            mFormatFlags: kAudioFormatFlagsNativeFloatPacked, mBytesPerPacket: 8, mFramesPerPacket: 1,
            mBytesPerFrame: 8, mChannelsPerFrame: 2, mBitsPerChannel: 32, mReserved: 0)
        var description: CMAudioFormatDescription?
        let status = CMAudioFormatDescriptionCreate(allocator: kCFAllocatorDefault, asbd: &asbd,
            layoutSize: 0, layout: nil, magicCookieSize: 0, magicCookie: nil, extensions: nil,
            formatDescriptionOut: &description)
        guard status == noErr, let description else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(status)) }
        format = description
        input = AVAssetWriterInput(mediaType: .audio, outputSettings: [
            AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 48_000,
            AVNumberOfChannelsKey: 2, AVEncoderBitRateKey: 160_000
        ], sourceFormatHint: description)
        input.expectsMediaDataInRealTime = true
        guard writer.canAdd(input) else { throw CocoaError(.featureUnsupported) }
        writer.add(input)
    }

    public func append(_ samples: [Float], at pts: CMTime) throws {
        guard !samples.isEmpty, samples.count.isMultiple(of: 2) else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(paramErr)) }
        let length = samples.count * MemoryLayout<Float>.stride
        var data: CMBlockBuffer?
        var status = CMBlockBufferCreateWithMemoryBlock(allocator: kCFAllocatorDefault, memoryBlock: nil,
            blockLength: length, blockAllocator: kCFAllocatorDefault, customBlockSource: nil, offsetToData: 0,
            dataLength: length, flags: 0, blockBufferOut: &data)
        guard status == noErr, let data else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(status)) }
        status = samples.withUnsafeBytes { bytes in
            CMBlockBufferReplaceDataBytes(with: bytes.baseAddress!, blockBuffer: data, offsetIntoDestination: 0, dataLength: length)
        }
        guard status == noErr else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(status)) }
        var sample: CMSampleBuffer?
        status = CMAudioSampleBufferCreateReadyWithPacketDescriptions(allocator: kCFAllocatorDefault,
            dataBuffer: data, formatDescription: format, sampleCount: samples.count / 2,
            presentationTimeStamp: pts, packetDescriptions: nil, sampleBufferOut: &sample)
        guard status == noErr, let sample else { throw NSError(domain: NSOSStatusErrorDomain, code: Int(status)) }
        guard input.append(sample) else { throw CocoaError(.fileWriteUnknown) }
    }
}
