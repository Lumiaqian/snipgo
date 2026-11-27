import AVFoundation

func makeStudio() throws -> any Studio { RecordingStudio() }
final class RecordingStudio: Studio {
  func export(
    src: String, dst: String, start: Double, end: Double, width: Int64, fps: Int64, mute: Bool,
    progress: @escaping @Sendable (Int64) -> Void
  ) async throws {
    let asset = AVURLAsset(url: URL(fileURLWithPath: src))
    let range = CMTimeRange(
      start: CMTime(seconds: start, preferredTimescale: 60000),
      end: CMTime(seconds: end, preferredTimescale: 60000))
    let composition = AVMutableComposition()
    guard let video = try await asset.loadTracks(withMediaType: .video).first,
      let dest = composition.addMutableTrack(
        withMediaType: .video, preferredTrackID: kCMPersistentTrackID_Invalid)
    else { throw CocoaError(.fileReadCorruptFile) }
    try dest.insertTimeRange(range, of: video, at: .zero)
    if !mute, let audio = try await asset.loadTracks(withMediaType: .audio).first,
      let out = composition.addMutableTrack(
        withMediaType: .audio, preferredTrackID: kCMPersistentTrackID_Invalid)
    {
      try out.insertTimeRange(
        CMTimeRangeGetIntersection(range, otherRange: try await audio.load(.timeRange)), of: audio,
        at: .zero)
    }
    let size = try await video.load(.naturalSize)
    let factor = width > 0 ? min(1, Double(width) / size.width, 1080 / size.height) : 1
    let vc = AVMutableVideoComposition()
    vc.renderSize = CGSize(
      width: floor(size.width * factor / 2) * 2, height: floor(size.height * factor / 2) * 2)
    vc.frameDuration = CMTime(value: 1, timescale: CMTimeScale(fps))
    let layer = AVMutableVideoCompositionLayerInstruction(assetTrack: dest)
    layer.setTransform(CGAffineTransform(scaleX: factor, y: factor), at: .zero)
    let instruction = AVMutableVideoCompositionInstruction()
    instruction.timeRange = CMTimeRange(start: .zero, duration: range.duration)
    instruction.layerInstructions = [layer]
    vc.instructions = [instruction]
    guard
      let session = AVAssetExportSession(
        asset: composition, presetName: AVAssetExportPresetHighestQuality)
    else { throw CocoaError(.featureUnsupported) }
    session.videoComposition = vc
    session.outputURL = URL(fileURLWithPath: dst)
    session.outputFileType = .mp4
    let monitor = Task {
      var previous: Int64 = -1
      while !Task.isCancelled {
        let value = Int64(session.progress * 100)
        if value != previous {
          progress(value)
          previous = value
        }
        try? await Task.sleep(for: .milliseconds(150))
      }
    }
    await withTaskCancellationHandler(
      operation: { await session.export() }, onCancel: { session.cancelExport() })
    monitor.cancel()
    await monitor.value
    guard session.status == .completed else { throw session.error ?? CocoaError(.userCancelled) }
  }
}
