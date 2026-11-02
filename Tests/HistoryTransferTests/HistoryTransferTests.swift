import CryptoKit
import Foundation
import SwiftProtobuf
import Testing
import HistoryTransfer
import SockContract

// Mirrors historytransfer_test.go, plus the two interop vector tests.

func seq32() -> Data { Data((0..<32).map { UInt8($0) }) }

func b64url(_ d: Data) -> String {
    d.base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

func b64std(_ d: Data) -> String {
    d.base64EncodedString().replacingOccurrences(of: "=", with: "")
}

@Test func testParsePayloadRoundTrip() throws {
    for host in ["api.joinsock.com", "staging-api.joinsock.com:8443", "localhost:8080"] {
        var digest = Data(count: 32)
        digest[0] = 0xff
        digest[31] = 0xee
        let want = try Payload(secret: seq32(), keyDigest: digest, host: host)
        let s = want.string
        #expect(s.hasPrefix("sock-history:1:") && !s.contains("="), "string = \(s)")
        let got = try Payload(parsing: s)
        #expect(got == want)
    }
}

@Test func testParsePayloadRejects() throws {
    let good = try Payload(secret: seq32(), keyDigest: seq32(), host: "api.joinsock.com").string
    let parts = good.split(separator: ":", maxSplits: 4, omittingEmptySubsequences: false).map(String.init)
    let zeros = { (n: Int) in b64url(Data(count: n)) }
    let fbff = Data(Array(repeating: [UInt8(0xfb), 0xff], count: 16).joined())
    let p2 = parts[2], p3 = parts[3], p4 = parts[4]
    let cases: [String: String] = [
        "wrong prefix": "sock-hist:1:" + parts[2...].joined(separator: ":"),
        "wrong version": "sock-history:2:" + parts[2...].joined(separator: ":"),
        "31-byte secret": "sock-history:1:\(zeros(31)):\(p3):\(p4)",
        "33-byte digest": "sock-history:1:\(p2):\(zeros(33)):\(p4)",
        "padded secret": "sock-history:1:\(p2)=:\(p3):\(p4)",
        "std alphabet": "sock-history:1:\(b64std(fbff)):\(p3):\(p4)",
        "LF in secret": "sock-history:1:\(p2.prefix(10))\n\(p2.dropFirst(10)):\(p3):\(p4)",
        "CR in digest": "sock-history:1:\(p2):\(p3.prefix(10))\r\(p3.dropFirst(10)):\(p4)",
        "CRLF in host": good + "\r\n",
        "empty host": "sock-history:1:\(p2):\(p3):",
        "missing host": "sock-history:1:\(p2):\(p3)",
        "empty": "",
        "non-canonical b64": "sock-history:1:\(p2.prefix(42))B:\(p3):\(p4)",
    ]
    let url = "sock-history:1:\(b64url(fbff)):\(p3):\(p4)"
    #expect(url.contains("-") || url.contains("_"), "alphabet case does not exercise - or _")
    #expect(throws: Never.self) { try Payload(parsing: url) }
    for (name, s) in cases {
        #expect(throws: (any Error).self, "\(name) accepted") { try Payload(parsing: s) }
    }
}

@Test func testParsePayloadHostAlias() {
    #expect(Payload.sameHost("api.sock.aeryx.ai", "api.joinsock.com"))
    #expect(Payload.sameHost("api.joinsock.com", "API.JoinSock.com"))
    #expect(Payload.sameHost("api.sock.aeryx.ai:443", "api.joinsock.com"))
    #expect(Payload.sameHost("api.sock.aeryx.ai:8443", "api.joinsock.com:8443"))
    #expect(!Payload.sameHost("api.sock.aeryx.ai:8443", "api.joinsock.com"))
    #expect(!Payload.sameHost("staging-api.joinsock.com", "api.joinsock.com"))
    #expect(!Payload.sameHost("staging-api.joinsock.com", "api.sock.aeryx.ai"))
}

let vectorAccount = UUID(uuidString: "6ba7b810-9dad-11d1-80b4-00c04fd430c8")!

func hex(_ d: Data) -> String { d.map { String(format: "%02x", $0) }.joined() }

@Test func testTagKnownAnswer() {
    // Same openssl-derived value as TestTagKnownAnswer in Go.
    let want = "7c3d0918b3d310a747516eb4e6fd02a66f78641fcdcc0b03a229f02ea52ae0d5"
    #expect(hex(tag(secret: seq32(), account: vectorAccount)) == want)
}

func newPair(tag: Data) throws -> (Sealer, Opener) {
    let (priv, pub) = try newRecipientKey()
    #expect(pub.count == recipientKeyLen)
    let s = try Sealer(recipientKey: pub, tag: tag)
    #expect(s.encapsulatedKey.count == encLen)
    let o = try Opener(privateKey: priv, encapsulatedKey: s.encapsulatedKey, tag: tag)
    return (s, o)
}

@Test func testSealOpenRoundTrip() throws {
    let (s, o) = try newPair(tag: seq32())
    let chunks = [Data("one".utf8), Data(repeating: 7, count: maxChunkPlaintext), Data("three".utf8)]
    for (i, pt) in chunks.enumerated() {
        let final = i == chunks.count - 1
        let ct = try s.seal(index: UInt32(i), final: final, pt)
        #expect(try o.open(index: UInt32(i), final: final, ct) == pt)
    }
    #expect(throws: (any Error).self, "seal after final") { try s.seal(index: 3, final: true, Data("x".utf8)) }
    #expect(throws: (any Error).self, "open after final") { try o.open(index: 3, final: true, Data("x".utf8)) }
}

@Test func testSealRefuses() throws {
    let (s, _) = try newPair(tag: seq32())
    let x = Data("x".utf8)
    #expect(throws: (any Error).self, "out of order") { try s.seal(index: 1, final: false, x) }
    #expect(throws: (any Error).self, "empty chunk") { try s.seal(index: 0, final: false, Data()) }
    #expect(throws: (any Error).self, "oversized chunk") {
        try s.seal(index: 0, final: false, Data(count: maxChunkPlaintext + 1))
    }
    for i in 0..<(maxChunks - 1) {
        _ = try s.seal(index: UInt32(i), final: false, x)
    }
    #expect(throws: (any Error).self, "last allowed chunk without final") {
        try s.seal(index: UInt32(maxChunks - 1), final: false, x)
    }
    #expect(throws: Never.self) { try s.seal(index: UInt32(maxChunks - 1), final: true, x) }
    #expect(throws: (any Error).self, "short recipient key") {
        try Sealer(recipientKey: Data(count: recipientKeyLen - 1), tag: seq32())
    }
}

@Test func testOpenRejectsWrongIndex() throws {
    let (s, o) = try newPair(tag: seq32())
    let ct = try s.seal(index: 0, final: false, Data("zero".utf8))
    #expect(throws: (any Error).self) { try o.open(index: 1, final: false, ct) }
    #expect(try o.open(index: 0, final: false, ct) == Data("zero".utf8))
}

@Test func testOpenRejectsFinalFlip() throws {
    let (s, o) = try newPair(tag: seq32())
    let ct = try s.seal(index: 0, final: false, Data("zero".utf8))
    #expect(throws: (any Error).self, "final flag flipped") { try o.open(index: 0, final: true, ct) }
    #expect(throws: (any Error).self, "open after a failure") { try o.open(index: 0, final: false, ct) }
    #expect(!o.done)
}

@Test func testOpenerDone() throws {
    let (s, o) = try newPair(tag: seq32())
    for i in 0..<2 {
        let final = i == 1
        let ct = try s.seal(index: UInt32(i), final: final, Data("x".utf8))
        #expect(!o.done, "done before chunk \(i)")
        _ = try o.open(index: UInt32(i), final: final, ct)
    }
    #expect(o.done)
}

@Test func testOpenRejectsOtherTag() throws {
    let (priv, pub) = try newRecipientKey()
    let s = try Sealer(recipientKey: pub, tag: seq32())
    let ct = try s.seal(index: 0, final: true, Data("zero".utf8))
    var other = seq32()
    other[0] ^= 1
    let o = try Opener(privateKey: priv, encapsulatedKey: s.encapsulatedKey, tag: other)
    #expect(throws: (any Error).self) { try o.open(index: 0, final: true, ct) }
}

@Test func testKeyDigest() throws {
    var (_, pub) = try newRecipientKey()
    let d = keyDigest(pub)
    #expect(d.count == 32)
    pub[0] ^= 1
    #expect(keyDigest(pub) != d)
}

typealias Rec = Sock_V1_HistoryRecord

func header(_ v: UInt32, _ id: String) -> Rec {
    var h = Sock_V1_HistoryHeader()
    h.formatVersion = v
    h.accountID = id
    var r = Rec()
    r.header = h
    return r
}

func account(_ id: String, _ name: String = "") -> Rec {
    var a = Sock_V1_Account()
    a.id = id
    a.displayName = name
    var r = Rec()
    r.account = a
    return r
}

func conversation(_ id: String) -> Rec {
    var c = Sock_V1_Conversation()
    c.id = id
    var r = Rec()
    r.conversation = c
    return r
}

func message(_ conv: String, _ seq: Int64) -> Rec {
    var m = Sock_V1_Message()
    m.conversationID = conv
    m.seq = seq
    var r = Rec()
    r.message = m
    return r
}

// Same records as sampleArchive in Go.
func sampleArchive(_ n: Int) -> [Rec] {
    var h = header(UInt32(formatVersion), "acct-1")
    h.header.exportedAt = Google_Protobuf_Timestamp(seconds: 1_793_534_400)  // 2026-11-01T12:00:00Z
    var recs = [h, account("acct-1", "Guy"), account("acct-2", "Puppet")]
    var conv = 0
    for i in 0..<n {
        if i % 100 == 0 {
            conv += 1
            var c = conversation("conv-\(conv)")
            c.conversation.memberIds = ["acct-1", "acct-2"]
            c.conversation.readThroughSeq = 3
            recs.append(c)
        }
        var m = message("conv-\(conv)", Int64(i % 100 + 1))
        m.message.id = "m-\(i)"
        m.message.senderID = "acct-2"
        recs.append(m)
    }
    return recs
}

func writeArchive(_ recs: [Rec]) throws -> Data {
    let out = OutputStream(toMemory: ())
    out.open()
    defer { out.close() }
    let w = ArchiveWriter(out)
    for r in recs {
        try w.write(r)
    }
    return out.property(forKey: .dataWrittenToMemoryStreamKey) as! Data
}

func readArchive(_ data: Data) throws -> [Rec] {
    let input = InputStream(data: data)
    input.open()
    defer { input.close() }
    var r = ArchiveReader(input)
    var order = OrderValidator()
    var out: [Rec] = []
    while let rec = try r.next() {
        try order.check(rec)
        out.append(rec)
    }
    return out
}

@Test func testArchiveRoundTrip() throws {
    let recs = sampleArchive(1000 - 3 - 10)
    #expect(recs.count == 1000)
    #expect(try readArchive(writeArchive(recs)) == recs)
}

@Test func testArchiveReaderTruncated() throws {
    let data = try writeArchive([sampleArchive(0)[0]])
    let input = InputStream(data: data.dropLast())
    input.open()
    defer { input.close() }
    var r = ArchiveReader(input)
    #expect(throws: (any Error).self) { try r.next() }
}

/// Feeds data to a fresh decoder in pieces cut at `cuts` and returns every record.
func decode(_ data: Data, cuts: [Int]) throws -> [Rec] {
    var d = ArchiveDecoder()
    var out: [Rec] = []
    var from = 0
    for cut in cuts + [data.count] {
        out += try d.feed(data.subdata(in: from..<cut))
        from = cut
    }
    try d.finish()
    return out
}

@Test func testArchiveDecoderEveryCut() throws {
    let recs = sampleArchive(5)
    let data = try writeArchive(recs)
    for cut in 0...data.count {
        #expect(try decode(data, cuts: [cut]) == recs, "cut at \(cut)")
    }
    #expect(try decode(data, cuts: Array(1..<data.count)) == recs, "a byte at a time")
}

@Test func testArchiveDecoderRecordsComeOutOfTheFeedThatCompletesThem() throws {
    let recs = sampleArchive(1)
    let first = try writeArchive([recs[0]])
    let data = try writeArchive(recs)
    var d = ArchiveDecoder()
    #expect(try d.feed(data.prefix(first.count - 1)).isEmpty)
    #expect(try d.feed(data.subdata(in: first.count - 1..<first.count)) == [recs[0]])
}

@Test func testArchiveDecoderCutInsideARecord() throws {
    let data = try writeArchive([sampleArchive(0)[0]])
    var d = ArchiveDecoder()
    #expect(try d.feed(data.dropLast()).isEmpty)
    #expect(throws: HistoryTransferError.self) { try d.finish() }
}

/// A varint prefix for n.
func varint(_ n: Int) -> Data {
    var n = n
    var out = Data()
    while n >= 0x80 {
        out.append(UInt8(n & 0x7F) | 0x80)
        n >>= 7
    }
    out.append(UInt8(n))
    return out
}

@Test func testArchiveDecoderCap() throws {
    // at the cap the decoder waits for the bytes; one over, it refuses the
    // prefix alone, before any of them arrive
    var atCap = ArchiveDecoder()
    #expect(try atCap.feed(varint(maxRecordBytes)).isEmpty)
    var over = ArchiveDecoder()
    #expect(throws: HistoryTransferError.self) { try over.feed(varint(maxRecordBytes + 1)) }
    var huge = ArchiveDecoder()
    #expect(throws: HistoryTransferError.self) { try huge.feed(Data(repeating: 0xFF, count: 11)) }
    // a 10 byte varint for 1 << 63 is Int.min as an Int: refused, not a trap
    var wraps = ArchiveDecoder()
    #expect(throws: HistoryTransferError.self) {
        try wraps.feed(Data([0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01]))
    }
    // a 10th byte above 1 overflows 64 bits, as Go's protowire refuses
    var tenth = ArchiveDecoder()
    #expect(throws: HistoryTransferError.self) {
        try tenth.feed(Data([0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x02]))
    }
    #expect(maxRecordBytes == 4 << 20)
}

@Test func testArchiveReaderCap() throws {
    // a well-formed record one byte over the cap
    let fits = try writeArchive([account("a", String(repeating: "x", count: maxRecordBytes - 16))])
    var big = account("a", String(repeating: "x", count: maxRecordBytes - 16))
    let body = try big.serializedData().count
    big.account.displayName += String(repeating: "x", count: maxRecordBytes + 1 - body)
    #expect(try decode(fits, cuts: []).count == 1)
    let input = InputStream(data: try writeArchive([big]))
    input.open()
    defer { input.close() }
    var r = ArchiveReader(input)
    #expect(throws: HistoryTransferError.self) { try r.next() }
}

@Test func testOrderValidator() throws {
    let hdr = header(1, "a")
    let acct = account("a")
    let ok: [String: [Rec]] = [
        "header only": [hdr],
        "accounts after header": [hdr, acct, acct],
        "conversation after header": [hdr, conversation("c")],
        "conversation after account": [hdr, acct, conversation("c")],
        "conversation after empty conversation": [hdr, conversation("c"), conversation("d")],
        "messages in rising seq": [hdr, conversation("c"), message("c", 1), message("c", 5)],
        "conversation after message": [hdr, conversation("c"), message("c", 1), conversation("d"), message("d", 1)],
    ]
    for (name, seq) in ok {
        var v = OrderValidator()
        for (i, r) in seq.enumerated() {
            #expect(throws: Never.self, "\(name): record \(i)") { try v.check(r) }
        }
    }
    let bad: [String: [Rec]] = [
        "account first": [acct],
        "newer format": [header(UInt32(formatVersion) + 1, "a")],
        "unset format": [header(0, "a")],
        "empty account id": [header(1, "")],
        "header after header": [hdr, hdr],
        "second header": [hdr, acct, hdr],
        "account after conversation": [hdr, conversation("c"), acct],
        "account after message": [hdr, conversation("c"), message("c", 1), acct],
        "message after header": [hdr, message("c", 1)],
        "message after account": [hdr, acct, message("c", 1)],
        "message of another conversation": [hdr, conversation("c"), message("d", 1)],
        "message switching conversation": [hdr, conversation("c"), message("c", 1), message("d", 2)],
        "first seq zero": [hdr, conversation("c"), message("c", 0)],
        "first seq negative": [hdr, conversation("c"), message("c", -1)],
        "seq repeated": [hdr, conversation("c"), message("c", 2), message("c", 2)],
        "seq falling": [hdr, conversation("c"), message("c", 2), message("c", 1)],
        "conversation repeated": [hdr, conversation("c"), message("c", 5), conversation("d"), conversation("c")],
        "empty record": [hdr, Rec()],
    ]
    for (name, seq) in bad {
        var v = OrderValidator()
        for (i, r) in seq.dropLast().enumerated() {
            try #require(throws: Never.self, "\(name): prefix record \(i)") { try v.check(r) }
        }
        #expect(throws: (any Error).self, "\(name) accepted") { try v.check(seq.last!) }
    }
}

// MARK: - Interop vectors

// Both vector files share one layout, documented beside the Go writer in
// historytransfer_test.go. All integers unsigned big-endian:
//
//   secret      32 bytes   transfer secret
//   account     16 bytes   human id UUID bytes
//   tag         32 bytes   tag(secret, account)
//   seed        32 bytes   X-Wing private key seed
//   public    1216 bytes   X-Wing public key from seed
//   enc       1120 bytes   HPKE encapsulated key
//   payloadLen  u32        then payloadLen bytes: Payload(secret, keyDigest(public), host).string
//   chunkCount  u32        then chunkCount chunks, each:
//     index     u32
//     final     u8         0x01 final, 0x00 not
//     ptLen     u32        then ptLen bytes of plaintext
//     ctLen     u32        then ctLen bytes of ciphertext
//
// go-sealed.bin is sealed by Go; swift-sealed.bin is sealed by Swift to the
// same public key, so Go opens it with go-sealed.bin's seed.

let vectorDir = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("testdata/history-transfer")

struct VectorChunk {
    var index: UInt32
    var final: Bool
    var plaintext: Data
    var ct: Data
}

struct Vector {
    var secret = Data()
    var account = Data()
    var tag = Data()
    var seed = Data()
    var publicKey = Data()
    var enc = Data()
    var payload = ""
    var chunks: [VectorChunk] = []

    init() {}

    init(_ data: Data) throws {
        var b = data[...]
        func take(_ n: Int) throws -> Data {
            guard b.count >= n else { throw VectorError("vector truncated") }
            let out = Data(b.prefix(n))
            b = b.dropFirst(n)
            return out
        }
        func u32() throws -> UInt32 { try take(4).reduce(0) { $0 << 8 | UInt32($1) } }
        secret = try take(32)
        account = try take(16)
        tag = try take(32)
        seed = try take(32)
        publicKey = try take(recipientKeyLen)
        enc = try take(encLen)
        guard let p = String(data: try take(Int(u32())), encoding: .utf8) else { throw VectorError("payload is not UTF-8") }
        payload = p
        for _ in 0..<(try u32()) {
            let index = try u32()
            let final = try take(1)[0] == 1
            let pt = try take(Int(u32()))
            let ct = try take(Int(u32()))
            chunks.append(VectorChunk(index: index, final: final, plaintext: pt, ct: ct))
        }
        guard b.isEmpty else { throw VectorError("vector has \(b.count) trailing bytes") }
    }

    func marshal() -> Data {
        func u32(_ x: Int) -> Data { withUnsafeBytes(of: UInt32(x).bigEndian) { Data($0) } }
        var d = secret + account + tag + seed + publicKey + enc
        d += u32(payload.utf8.count) + Data(payload.utf8)
        d += u32(chunks.count)
        for c in chunks {
            d += u32(Int(c.index)) + Data([c.final ? 1 : 0])
            d += u32(c.plaintext.count) + c.plaintext + u32(c.ct.count) + c.ct
        }
        return d
    }
}

struct VectorError: Error, CustomStringConvertible {
    let description: String
    init(_ d: String) { description = d }
}

/// Checks everything in v that does not need the ciphertext's author, then
/// opens its chunks and returns the archive's records.
func openVector(_ v: Vector) throws -> [Rec] {
    let account = UUID(uuid: v.account.withUnsafeBytes { $0.loadUnaligned(as: uuid_t.self) })
    #expect(tag(secret: v.secret, account: account) == v.tag, "tag does not match tag(secret, account)")
    let priv = try XWingMLKEM768X25519.PrivateKey(seedRepresentation: v.seed, publicKey: nil)
    #expect(priv.publicKey.rawRepresentation == v.publicKey, "public key does not derive from the seed")
    let p = try Payload(parsing: v.payload)
    #expect(p.secret == v.secret && p.keyDigest == keyDigest(v.publicKey))
    let o = try Opener(privateKey: priv, encapsulatedKey: v.enc, tag: v.tag)
    var archive = Data()
    for c in v.chunks {
        let pt = try o.open(index: c.index, final: c.final, c.ct)
        #expect(pt == c.plaintext, "chunk \(c.index) plaintext differs")
        archive += pt
    }
    #expect(o.done)
    return try readArchive(archive)
}

@Test func testOpenGoVector() throws {
    let v = try Vector(Data(contentsOf: vectorDir.appendingPathComponent("go-sealed.bin")))
    #expect(v.chunks.count == 2)
    #expect(try openVector(v) == sampleArchive(5))
}

/// Go and Swift write the same archive bytes and the same code for the same
/// input: the Go vector's plaintext is Go's ArchiveWriter output.
@Test func testArchiveAndPayloadBytesMatchGo() throws {
    let go = try Vector(Data(contentsOf: vectorDir.appendingPathComponent("go-sealed.bin")))
    let goArchive = go.chunks.reduce(Data()) { $0 + $1.plaintext }
    #expect(try writeArchive(sampleArchive(5)) == goArchive)
    let host = try Payload(parsing: go.payload).host
    #expect(try Payload(secret: go.secret, keyDigest: keyDigest(go.publicKey), host: host).string == go.payload)
}

@Test func testWriteSwiftVector() throws {
    let path = vectorDir.appendingPathComponent("swift-sealed.bin")
    if ProcessInfo.processInfo.environment["UPDATE_VECTORS"] == "1" {
        let go = try Vector(Data(contentsOf: vectorDir.appendingPathComponent("go-sealed.bin")))
        var v = Vector()
        v.secret = go.secret
        v.account = go.account
        v.tag = tag(secret: go.secret, account: vectorAccount)
        v.seed = go.seed
        v.publicKey = go.publicKey
        v.payload = try Payload(secret: v.secret, keyDigest: keyDigest(v.publicKey), host: "staging-api.joinsock.com:8443").string
        let s = try Sealer(recipientKey: v.publicKey, tag: v.tag)
        v.enc = s.encapsulatedKey
        let archive = try writeArchive(sampleArchive(5))
        let cut = archive.count / 2 + 1
        for (i, pt) in [archive.prefix(cut), archive.dropFirst(cut)].enumerated() {
            let pt = Data(pt)
            let final = i == 1
            v.chunks.append(VectorChunk(index: UInt32(i), final: final, plaintext: pt, ct: try s.seal(index: UInt32(i), final: final, pt)))
        }
        try v.marshal().write(to: path)
    }
    let v = try Vector(Data(contentsOf: path))
    #expect(try openVector(v) == sampleArchive(5))
}
