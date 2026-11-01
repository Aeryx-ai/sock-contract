import Foundation

private let payloadPrefix = "sock-history"
private let payloadVersion = "1"

/// Maps every alias of a hub host to its one name.
private let hostAliases = ["api.sock.aeryx.ai": "api.joinsock.com"]

/// What the desktop shows as a QR code and the phone scans.
public struct Payload: Equatable, Sendable {
    public let secret: Data
    public let keyDigest: Data
    /// The hub's API host as the desktop is configured, port included when
    /// there is one.
    public let host: String

    /// Refuses a secret or key digest that is not 32 bytes and an empty host.
    public init(secret: Data, keyDigest: Data, host: String) throws {
        guard secret.count == secretLen else { throw HistoryTransferError("secret: \(secret.count) bytes, want \(secretLen)") }
        guard keyDigest.count == 32 else { throw HistoryTransferError("key digest: \(keyDigest.count) bytes, want 32") }
        guard !host.isEmpty else { throw HistoryTransferError("empty host") }
        self.secret = Data(secret)
        self.keyDigest = Data(keyDigest)
        self.host = host
    }

    /// Decodes a scanned payload. The host is everything after the fourth
    /// colon, so a host:port parses.
    public init(parsing s: String) throws {
        // Scalars, not Characters: "\r\n" is one Character.
        guard !s.unicodeScalars.contains(where: { $0 == "\r" || $0 == "\n" }) else {
            throw HistoryTransferError("line break in code")
        }
        let parts = s.split(separator: ":", maxSplits: 4, omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 5, parts[0] == payloadPrefix else { throw HistoryTransferError("not a history transfer code") }
        guard parts[1] == payloadVersion else { throw HistoryTransferError("unsupported code version \"\(parts[1])\"") }
        guard let secret = decodeB64URL(parts[2]) else { throw HistoryTransferError("secret: not canonical unpadded base64url") }
        guard let digest = decodeB64URL(parts[3]) else { throw HistoryTransferError("key digest: not canonical unpadded base64url") }
        try self.init(secret: secret, keyDigest: digest, host: parts[4])
    }

    /// sock-history:1:<secret>:<key digest>:<host>.
    public var string: String {
        [payloadPrefix, payloadVersion, encodeB64URL(secret), encodeB64URL(keyDigest), host].joined(separator: ":")
    }

    /// Reports whether a and b name the same hub once aliases are mapped on the
    /// host name. Ports must match; a missing port is 443.
    public static func sameHost(_ a: String, _ b: String) -> Bool {
        canonicalHost(a) == canonicalHost(b)
    }
}

private func encodeB64URL(_ d: Data) -> String {
    d.base64EncodedString()
        .replacingOccurrences(of: "+", with: "-")
        .replacingOccurrences(of: "/", with: "_")
        .replacingOccurrences(of: "=", with: "")
}

/// Decodes unpadded base64url, refusing anything encodeB64URL would not have
/// written: padding, the standard alphabet and nonzero trailing bits, as Go's
/// RawURLEncoding.Strict does.
private func decodeB64URL(_ s: String) -> Data? {
    var std = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
    std += String(repeating: "=", count: (4 - std.count % 4) % 4)
    guard let d = Data(base64Encoded: std), encodeB64URL(d) == s else { return nil }
    return d
}

private func canonicalHost(_ h: String) -> String {
    var (name, port) = splitHostPort(h) ?? (h, "443")
    name = name.lowercased()
    name = hostAliases[name] ?? name
    return name.contains(":") ? "[\(name)]:\(port)" : "\(name):\(port)"
}

/// Go's net.SplitHostPort: host:port or [host]:port, nil when h is neither.
private func splitHostPort(_ h: String) -> (String, String)? {
    guard let colon = h.lastIndex(of: ":") else { return nil }
    var name = h[..<colon]
    let port = String(h[h.index(after: colon)...])
    if name.hasPrefix("[") {
        guard name.hasSuffix("]") else { return nil }
        name = name.dropFirst().dropLast()
        guard !name.contains("[") && !name.contains("]") else { return nil }
    } else if name.contains(":") || name.contains("[") || name.contains("]") {
        return nil
    }
    guard !port.contains("[") && !port.contains("]") else { return nil }
    return (String(name), port)
}
