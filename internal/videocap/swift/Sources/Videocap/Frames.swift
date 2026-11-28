import AVFoundation
import CoreImage


final class MovieFrames {
  private let render = CIContext(options: [.cacheIntermediates: false])
  private var reader: AVAssetReader?
  private var output: AVAssetReaderTrackOutput?
  private var sample: CMSampleBuffer?
  private var following: CMSampleBuffer?
  private var width = 0, height = 0, index = 0, count = 0
  private var start = 0.0, fps = 15.0
  func open(path: String, start: Double, end: Double, width: Int64, fps: Int64) async throws
    -> [Int64]
  {
    let asset = AVURLAsset(url: URL(fileURLWithPath: path))
    let duration = try await asset.load(.duration).seconds
    let end = end == 0 ? duration : min(end, duration)
    guard end.isFinite, start >= 0, end > start, width > 0, fps > 0 else {
      throw CocoaError(.validationNumberTooSmall)
    }
    reader?.cancelReading()
    guard let track = try await asset.loadTracks(withMediaType: .video).first else {
      throw CocoaError(.fileReadCorruptFile)
    }
    let size = try await track.load(.naturalSize)
    self.width = min(Int(width), Int(size.width))
    height = max(1, Int((size.height * Double(self.width) / size.width).rounded()))
    let r = try AVAssetReader(asset: asset)
    let out = AVAssetReaderTrackOutput(
      track: track,
      outputSettings: [kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA])
    out.alwaysCopiesSampleData = false
    r.add(out)
    r.timeRange = CMTimeRange(
      start: CMTime(seconds: start, preferredTimescale: 60000),
      end: CMTime(seconds: end, preferredTimescale: 60000))
    guard r.startReading() else { throw r.error ?? CocoaError(.fileReadCorruptFile) }
    self.reader = r
    self.output = out
    sample = out.copyNextSampleBuffer()
    following = out.copyNextSampleBuffer()
    guard sample != nil else { throw r.error ?? CocoaError(.fileReadCorruptFile) }
    index = 0
    count = Int(ceil((end - start) * Double(fps)))
    self.fps = Double(fps)
    self.start = start
    return [Int64(self.width), Int64(height), Int64(count)]
  }
  func next() throws -> Data {
    guard index < count else { return Data() }
    let t = start + Double(index) / fps
    while let next = following, next.presentationTimeStamp.seconds <= t {
      sample = next
      following = output?.copyNextSampleBuffer()
    }
    if reader?.status == .failed { throw reader?.error ?? CocoaError(.fileReadCorruptFile) }
    guard let buffer = sample?.imageBuffer else { throw CocoaError(.fileReadCorruptFile) }
    let ci = CIImage(cvPixelBuffer: buffer)
    let image = ci.transformed(
      by: CGAffineTransform(
        scaleX: Double(width) / ci.extent.width, y: Double(height) / ci.extent.height))
    var bytes = Data(count: width * height * 4)
    bytes.withUnsafeMutableBytes { dst in
      render.render(
        image, toBitmap: dst.baseAddress!, rowBytes: width * 4,
        bounds: CGRect(x: 0, y: 0, width: width, height: height), format: .RGBA8,
        colorSpace: CGColorSpace(name: CGColorSpace.sRGB))
    }
    index += 1
    return bytes
  }
}
