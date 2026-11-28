import CoreMedia
import Testing
import CaptureAudio

@Test func gapsOverlapAndLateArrival() {
    var mixer = AudioTimeline()
    mixer.insert([0.75, -0.75, 0.5, -0.5], from: .system, at: 1)
    mixer.insert([0.75, -0.75], from: .microphone, at: 1)
    #expect(mixer.render(frames: 2) == [0, 0, 1, -1])
    mixer.insert([1, 1], from: .microphone, at: 0)
    #expect(mixer.render(frames: 2) == [0.5, -0.5, 0, 0])
}

@Test func pauseIsRemovedBeforeConversionToFrames() {
    let seconds = { (value: Int64) in CMTime(value: value, timescale: 1) }
    #expect(framePosition(hostTime: seconds(111), origin: seconds(100), paused: seconds(10)) == 48_000)
}
