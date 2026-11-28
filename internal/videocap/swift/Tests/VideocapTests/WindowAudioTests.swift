import Testing
@testable import Videocap

@Test func windowAudioScopeMustMatch() throws {
    try verifyWindowAudio(owner: 11, windowID: 22, includedWindows: [22], includedApps: [11])
    #expect(throws: WindowAudioError.self) { try verifyWindowAudio(owner: 11, windowID: 22, includedWindows: [22], includedApps: []) }
    #expect(throws: WindowAudioError.self) { try verifyWindowAudio(owner: nil, windowID: 22, includedWindows: [22], includedApps: []) }
    #expect(throws: WindowAudioError.self) { try verifyWindowAudio(owner: 11, windowID: 22, includedWindows: [], includedApps: [11]) }
    #expect(throws: WindowAudioError.self) { try verifyWindowAudio(owner: 11, windowID: 22, includedWindows: [22], includedApps: [11, 33]) }
}
