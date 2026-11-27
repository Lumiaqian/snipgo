import AVFoundation
import Testing
import CaptureAudio

@Test func writeMixedAAC() async throws {
    let url = URL(fileURLWithPath: "/tmp/reelgo-audio-\(UUID()).mp4")
    defer { try? FileManager.default.removeItem(at: url) }
    let writer = try AVAssetWriter(outputURL: url, fileType: .mp4)
    let track = try AACTrack(writer: writer)
    guard writer.startWriting() else { throw writer.error ?? CocoaError(.fileWriteUnknown) }
    writer.startSession(atSourceTime: .zero)
    var mixer = AudioTimeline()
    for start in stride(from: 0, to: 48_000, by: 480) {
        for (source, frequency) in [(AudioTimeline.Source.system, 440.0), (.microphone, 880.0)] {
            let tone = (start..<(start+480)).flatMap { i -> [Float] in
                let v = Float(0.2 * sin(2 * .pi * frequency * Double(i) / 48_000))
                return [v,v]
            }
            mixer.insert(tone, from: source, at: Int64(start))
        }
        while !track.input.isReadyForMoreMediaData {
            guard writer.status == .writing else { throw writer.error ?? CocoaError(.fileWriteUnknown) }
            try await Task.sleep(for: .milliseconds(5))
        }
        try track.append(mixer.render(frames: 480), at: CMTime(value: Int64(start), timescale: 48_000))
    }
    track.input.markAsFinished()
    await writer.finishWriting()
    guard writer.status == .completed else { throw writer.error ?? CocoaError(.fileWriteUnknown) }
    let asset = AVURLAsset(url: url)
    let tracks = try await asset.loadTracks(withMediaType: .audio)
    #expect(tracks.count == 1)
    let duration = try await asset.load(.duration)
    #expect(abs(duration.seconds - 1) < 0.001)
}
