import CryptoKit
import Foundation

/// The X-Wing public key length.
public let recipientKeyLen = 1216
/// The X-Wing encapsulated key length.
public let encLen = 1120
/// Caps one chunk's plaintext.
public let maxChunkPlaintext = 1 << 20
/// Caps a transfer's chunk count.
public let maxChunks = 128

/// HPKE base mode, X-Wing, HKDF-SHA256, AES-256-GCM.
private let suite = HPKE.Ciphersuite.XWingMLKEM768X25519_SHA256_AES_GCM_256

/// Generates the desktop's key pair. The private key's seedRepresentation is
/// its 32-byte X-Wing seed, the form Go's PrivateKey.Bytes uses.
public func newRecipientKey() throws -> (XWingMLKEM768X25519.PrivateKey, Data) {
    let k = try XWingMLKEM768X25519.PrivateKey.generate()
    return (k, k.publicKey.rawRepresentation)
}

/// The SHA-256 of a recipient key, carried in the QR payload.
public func keyDigest(_ publicKey: Data) -> Data {
    Data(SHA256.hash(data: publicKey))
}

private func info(_ tag: Data) throws -> Data {
    guard tag.count == 32 else { throw HistoryTransferError("tag is \(tag.count) bytes, want 32") }
    return Data(label.utf8) + tag
}

/// uint32_be(index) ‖ final byte.
private func aad(_ index: UInt32, _ final: Bool) -> Data {
    withUnsafeBytes(of: index.bigEndian) { Data($0) } + [final ? 1 : 0]
}

/// The bookkeeping Sealer and Opener share: chunks go in index order from 0
/// and nothing follows the final one.
private struct ChunkOrder {
    var next: UInt32 = 0
    var done = false

    func check(_ index: UInt32, _ final: Bool) throws {
        if done { throw HistoryTransferError("chunk after the final one") }
        if index != next { throw HistoryTransferError("chunk \(index), want \(next)") }
        if index >= maxChunks { throw HistoryTransferError("chunk \(index) past the limit of \(maxChunks)") }
        if index == maxChunks - 1 && !final {
            throw HistoryTransferError("chunk \(index) is the last allowed and not final")
        }
    }

    mutating func advance(_ final: Bool) {
        next += 1
        done = final
    }
}

/// Seals an archive's chunks for one recipient key. The HPKE sequence number
/// supplies each chunk's nonce, so chunks are sealed in index order.
public final class Sealer {
    private var sender: HPKE.Sender
    private var order = ChunkOrder()
    /// Goes to the hub in ClaimHistoryTransfer.
    public let encapsulatedKey: Data

    /// Starts a sending context to recipientKey, bound to tag.
    public init(recipientKey: Data, tag: Data) throws {
        guard recipientKey.count == recipientKeyLen else {
            throw HistoryTransferError("recipient key is \(recipientKey.count) bytes, want \(recipientKeyLen)")
        }
        let pk: XWingMLKEM768X25519.PublicKey
        do {
            pk = try XWingMLKEM768X25519.PublicKey(rawRepresentation: recipientKey)
        } catch {
            throw HistoryTransferError("recipient key: \(error)")
        }
        let info = try info(tag)
        do {
            sender = try HPKE.Sender(recipientKey: pk, ciphersuite: suite, info: info)
        } catch {
            throw HistoryTransferError("sender: \(error)")
        }
        encapsulatedKey = sender.encapsulatedKey
    }

    /// Seals chunk index. Refuses an index that is not the next one, a chunk
    /// after the final one, an empty or oversized plaintext and a last allowed
    /// chunk that is not final.
    public func seal(index: UInt32, final: Bool, _ plaintext: Data) throws -> Data {
        guard (1...maxChunkPlaintext).contains(plaintext.count) else {
            throw HistoryTransferError("chunk plaintext is \(plaintext.count) bytes, want 1 to \(maxChunkPlaintext)")
        }
        try order.check(index, final)
        let ct: Data
        do {
            ct = try sender.seal(plaintext, authenticating: aad(index, final))
        } catch {
            throw HistoryTransferError("seal chunk \(index): \(error)")
        }
        order.advance(final)
        return ct
    }
}

/// Opens an archive's chunks with the desktop's private key.
public final class Opener {
    private var recipient: HPKE.Recipient
    private var order = ChunkOrder()
    private var failed = false

    /// Starts a receiving context from the phone's encapsulated key, bound to
    /// tag.
    public init(privateKey: XWingMLKEM768X25519.PrivateKey, encapsulatedKey: Data, tag: Data) throws {
        guard encapsulatedKey.count == encLen else {
            throw HistoryTransferError("encapsulated key is \(encapsulatedKey.count) bytes, want \(encLen)")
        }
        let info = try info(tag)
        do {
            recipient = try HPKE.Recipient(privateKey: privateKey, ciphersuite: suite, info: info, encapsulatedKey: encapsulatedKey)
        } catch {
            throw HistoryTransferError("recipient: \(error)")
        }
    }

    /// Opens chunk index. Refuses an index that is not the next one and a
    /// chunk after the final one, leaving the context where it was. A chunk
    /// that fails authentication ends the opener: every later open is refused.
    public func open(index: UInt32, final: Bool, _ ciphertext: Data) throws -> Data {
        if failed { throw HistoryTransferError("opener stopped after a chunk failed authentication") }
        try order.check(index, final)
        let pt: Data
        do {
            pt = try recipient.open(ciphertext, authenticating: aad(index, final))
        } catch {
            failed = true
            throw HistoryTransferError("open chunk \(index): \(error)")
        }
        order.advance(final)
        return pt
    }

    /// Whether the final chunk has opened. A stream that ends before done is
    /// truncated and must be refused.
    public var done: Bool { order.done }
}
