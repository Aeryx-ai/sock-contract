import CryptoKit
import Foundation

/// Prefixes the binding tag message and the HPKE info.
public let label = "sock history transfer v1"

/// The transfer secret's length in bytes.
public let secretLen = 32

/// HMAC-SHA-256(key = secret, msg = label ‖ account UUID bytes). The hub looks
/// a transfer up by it; both devices bind ciphertext to it. The secret must be
/// secretLen bytes; Payload guarantees that for a scanned one.
public func tag(secret: Data, account: UUID) -> Data {
    precondition(secret.count == secretLen, "history transfer secret is \(secret.count) bytes, want \(secretLen)")
    var m = HMAC<SHA256>(key: SymmetricKey(data: secret))
    m.update(data: Data(label.utf8))
    m.update(data: withUnsafeBytes(of: account.uuid) { Data($0) })
    return Data(m.finalize())
}

/// The error every check in this module throws, worded as Go's is.
public struct HistoryTransferError: Error, CustomStringConvertible, Sendable {
    public let description: String
    init(_ description: String) { self.description = "historytransfer: " + description }
}
