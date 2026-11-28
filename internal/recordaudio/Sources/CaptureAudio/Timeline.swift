import CoreMedia

/// Audio positions share a host-clock origin with video and exclude paused time.
public func framePosition(hostTime: CMTime, origin: CMTime, paused: CMTime) -> Int64? {
    let time = hostTime - origin - paused
    guard time.isNumeric else { return nil }
    return CMTimeConvertScale(time, timescale: 48_000, method: .roundHalfAwayFromZero).value
}

/// Owned by the recording queue. Input consists of normalized interleaved stereo PCM.
/// Render after the capture jitter allowance expires, or immediately when draining on stop.
public struct AudioTimeline {
    public enum Source: Hashable { case system, microphone }
    private struct Block {
        let start: Int64
        let samples: [Float]
        var end: Int64 { start + Int64(samples.count / 2) }
    }
    private var pending: [Source: [Block]] = [:]
    public private(set) var position: Int64 = 0

    public init(startFrame: Int64 = 0) { position = startFrame }

    public mutating func insert(_ samples: [Float], from source: Source, at start: Int64) {
        guard !samples.isEmpty, samples.count.isMultiple(of: 2),
              start <= Int64.max - Int64(samples.count / 2) else { return }
        let block = Block(start: start, samples: samples)
        guard block.end > position else { return }
        pending[source, default: []].append(block)
    }

    public mutating func render(frames: Int) -> [Float] {
        precondition(frames >= 0 && frames <= Int.max / 2)
        let end = position + Int64(frames)
        var mixed = [Float](repeating: 0, count: frames * 2)
        for source in [Source.system, .microphone] {
            guard let blocks = pending[source] else { continue }
            var channel = [Float](repeating: 0, count: mixed.count)
            // A later sample from the same device replaces overlap after clock correction.
            for block in blocks {
                let first = max(position, block.start)
                let last = min(end, block.end)
                guard first < last else { continue }
                let offset = Int(first - block.start) * 2
                let dest = Int(first - position) * 2
                let count = Int(last - first) * 2
                channel.replaceSubrange(dest..<(dest + count), with: block.samples[offset..<(offset + count)])
            }
            for i in mixed.indices { mixed[i] += channel[i] }
            pending[source] = blocks.filter { $0.end > end }
        }
        position = end
        return mixed.map { min(1, max(-1, $0)) }
    }
}
