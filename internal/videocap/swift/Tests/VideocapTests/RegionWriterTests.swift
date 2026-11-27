import AVFoundation
import Testing
@testable import Videocap

// Exercise the production writer without capturing the desktop or opening any input device.
@Test(arguments: [false, true])
func audioTrackAppearsOnlyAfterSamples(enableLate: Bool) async throws {
    let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".mp4")
    defer { try? FileManager.default.removeItem(at: file) }
    let sink = try VideoWriter(url: file, width: 64, height: 48)
    var pixel: CVPixelBuffer?
    let attrs = [kCVPixelBufferIOSurfacePropertiesKey: [:]] as CFDictionary
    #expect(CVPixelBufferCreate(nil, 64, 48, kCVPixelFormatType_32BGRA, attrs, &pixel) == kCVReturnSuccess)
    let buffer = try #require(pixel)
    var format: CMVideoFormatDescription?
    CMVideoFormatDescriptionCreateForImageBuffer(allocator: nil, imageBuffer: buffer, formatDescriptionOut: &format)
    for frame in 0..<15 {
        let pts = CMTime(value: Int64(frame), timescale: 10)
        var timing = CMSampleTimingInfo(duration: CMTime(value: 1, timescale: 10), presentationTimeStamp: pts, decodeTimeStamp: .invalid)
        var sample: CMSampleBuffer?
        CMSampleBufferCreateReadyWithImageBuffer(allocator: nil, imageBuffer: buffer, formatDescription: try #require(format),
                                                 sampleTiming: &timing, sampleBufferOut: &sample)
        sink.appendVideo(try #require(sample))
        if enableLate && frame >= 5 && frame < 10 {
            sink.receive(Array(repeating: 0.1, count: 9600), from: .microphone, at: pts)
        }
        try await Task.sleep(for: .milliseconds(20))
    }
    try sink.finish(at: CMTime(value: 15, timescale: 10))
    let asset = AVURLAsset(url: file)
    let video = try await asset.loadTracks(withMediaType: .video)
    let tracks = try await asset.loadTracks(withMediaType: .audio)
    #expect(video.count == 1)
    #expect(tracks.count == (enableLate ? 1 : 0))
    if let audio = tracks.first {
        let interval = try await audio.load(.timeRange)
        // Containers may represent leading silence with an empty edit beginning at zero.
        let reader = try AVAssetReader(asset: asset)
        let out = AVAssetReaderTrackOutput(track: audio, outputSettings: [
            AVFormatIDKey: kAudioFormatLinearPCM, AVLinearPCMIsFloatKey: true,
            AVLinearPCMBitDepthKey: 32, AVLinearPCMIsNonInterleaved: false])
        reader.add(out)
        #expect(reader.startReading())
        var onset: Double?
        while let sample = out.copyNextSampleBuffer(), onset == nil {
            guard let data = sample.dataBuffer else { continue }
            let length = CMBlockBufferGetDataLength(data)
            var values = [Float](repeating: 0, count: length / 4)
            let status = values.withUnsafeMutableBytes {
                CMBlockBufferCopyDataBytes(data, atOffset: 0, dataLength: length, destination: $0.baseAddress!)
            }
            #expect(status == noErr)
            if let i = values.firstIndex(where: { abs($0) > 0.015 }) {
                onset = sample.presentationTimeStamp.seconds + Double(i / 2) / 48_000
            }
        }
        #expect(abs((onset ?? -1) - 0.5) < 0.05)
        #expect(abs(interval.end.seconds - 1.5) < 0.05)
    }
}
