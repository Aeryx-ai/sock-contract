import Foundation
import SwiftProtobuf
import Testing
import SockContract

@Test func notifyOwnerEncodesBareText() throws {
    var req = Sock_V1_NotifyOwnerRequest()
    req.text = "build done 🧦"
    let json = try req.jsonString()
    #expect(json == #"{"text":"build done 🧦"}"#)
}

@Test func apnsTokenUsesSharedBase64Fixture() throws {
    var req = Sock_V1_RegisterPushRequest()
    req.apnsToken = Data([0xde, 0xad, 0xbe, 0xef])
    req.environment = .sandbox
    let json = try req.jsonString()
    // Same fixture as wire_test.go apnsTokenFixtureB64.
    #expect(json.contains(#""apnsToken":"3q2+7w==""#))
    #expect(json.contains(#""environment":"APNS_ENVIRONMENT_SANDBOX""#))
}

@Test func messageDecodesServerJSON() throws {
    let json = #"{"id":"m1","conversationId":"c1","seq":"7","senderId":"a1","sentAt":"2026-10-10T22:00:00Z","text":"hi"}"#
    let m = try Sock_V1_Message(jsonString: json)
    #expect(m.seq == 7)
    #expect(m.text == "hi")
}
