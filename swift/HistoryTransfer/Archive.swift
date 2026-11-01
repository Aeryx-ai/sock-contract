import Foundation
import SockContract
import SwiftProtobuf

/// The archive format this build writes and the highest it reads.
public let formatVersion = 1

/// Writes HistoryRecords, each prefixed by its varint length.
public struct ArchiveWriter {
    private let stream: OutputStream

    /// The stream must already be open.
    public init(_ stream: OutputStream) { self.stream = stream }

    public func write(_ record: Sock_V1_HistoryRecord) throws {
        do {
            try BinaryDelimited.serialize(message: record, to: stream)
        } catch {
            throw HistoryTransferError("write record: \(error)")
        }
    }
}

/// The longest record an archive may hold, as Go's ArchiveReader (protodelim's
/// default MaxSize). A longer length prefix is refused before its bytes arrive.
public let maxRecordBytes = 4 << 20

/// Decodes an archive's records from bytes fed in any cut: a record split
/// across feeds comes out of the feed that completes it.
public struct ArchiveDecoder {
    private var pending = Data()

    public init() {}

    /// Takes the next bytes and returns every record they complete, in order.
    /// Throws on a length prefix that is not a varint or over maxRecordBytes,
    /// and on a record that does not decode.
    public mutating func feed(_ bytes: Data) throws -> [Sock_V1_HistoryRecord] {
        pending.append(bytes)
        var records: [Sock_V1_HistoryRecord] = []
        var at = pending.startIndex
        while let (length, body) = try Self.prefix(pending, at), body + length <= pending.endIndex {
            do {
                records.append(try Sock_V1_HistoryRecord(serializedBytes: pending[body..<body + length]))
            } catch {
                throw HistoryTransferError("read record: \(error)")
            }
            at = body + length
        }
        pending = Data(pending[at...])
        return records
    }

    /// After the last bytes: throws when they end inside a record.
    public func finish() throws {
        guard pending.isEmpty else { throw HistoryTransferError("read record: archive ends inside a record") }
    }

    /// The record length at `at` and where its bytes start, or nil while the
    /// length itself is still cut short.
    private static func prefix(_ data: Data, _ at: Data.Index) throws -> (Int, Data.Index)? {
        var length = 0
        var i = at
        for shift in stride(from: 0, to: 64, by: 7) {
            guard i < data.endIndex else { return nil }
            let b = data[i]
            i += 1
            length |= Int(b & 0x7F) << shift
            guard length <= maxRecordBytes else {
                throw HistoryTransferError("read record: longer than \(maxRecordBytes) bytes")
            }
            if b & 0x80 == 0 { return (length, i) }
        }
        throw HistoryTransferError("read record: length is not a varint")
    }
}

/// Reads the records an ArchiveWriter wrote from a stream, through
/// ArchiveDecoder.
public struct ArchiveReader {
    private let stream: InputStream
    private var decoder = ArchiveDecoder()
    private var ready: [Sock_V1_HistoryRecord] = []
    private var buffer = [UInt8](repeating: 0, count: 64 << 10)

    /// The stream must already be open.
    public init(_ stream: InputStream) { self.stream = stream }

    /// Returns the next record, or nil after the last one. A stream cut inside
    /// a record is an error, not nil.
    public mutating func next() throws -> Sock_V1_HistoryRecord? {
        while ready.isEmpty {
            let n = stream.read(&buffer, maxLength: buffer.count)
            if n < 0 { throw HistoryTransferError("read record: \(stream.streamError.map { "\($0)" } ?? "stream error")") }
            if n == 0 {
                try decoder.finish()
                return nil
            }
            ready = try decoder.feed(Data(buffer[..<n]))
        }
        return ready.removeFirst()
    }
}

/// Checks an archive's record order, one record at a time: one header first,
/// then accounts, then each conversation once, followed by its messages in
/// rising seq from 1 up. A new value is ready for the first record. A refused
/// record leaves the validator where it was.
public struct OrderValidator {
    private var prev: Sock_V1_HistoryRecord.OneOf_Record?
    private var seen = Set<String>()

    public init() {}

    /// Throws unless next may follow the records already checked.
    public mutating func check(_ next: Sock_V1_HistoryRecord) throws {
        try validate(next)
        if case .conversation(let c)? = next.record {
            seen.insert(c.id)
        }
        prev = next.record
    }

    private func validate(_ next: Sock_V1_HistoryRecord) throws {
        switch next.record {
        case .header(let h)?:
            if prev != nil { throw HistoryTransferError("header after the first record") }
            if h.formatVersion == 0 || h.formatVersion > formatVersion {
                throw HistoryTransferError("archive format \(h.formatVersion), this build reads 1 to \(formatVersion)")
            }
            if h.accountID.isEmpty { throw HistoryTransferError("header has no account id") }
        case nil:
            throw HistoryTransferError("empty record")
        case .account?:
            switch prev {
            case .header?, .account?: return
            case nil: throw HistoryTransferError("archive does not start with a header")
            default: throw HistoryTransferError("account after a conversation")
            }
        case .conversation(let c)?:
            if prev == nil { throw HistoryTransferError("archive does not start with a header") }
            if seen.contains(c.id) { throw HistoryTransferError("conversation \"\(c.id)\" repeated") }
        case .message(let m)?:
            switch prev {
            case .conversation(let c)?:
                if m.conversationID != c.id { throw HistoryTransferError("message outside the current conversation") }
                if m.seq <= 0 { throw HistoryTransferError("first message seq \(m.seq), want 1 or more") }
            case .message(let p)?:
                if m.conversationID != p.conversationID { throw HistoryTransferError("message outside the current conversation") }
                if m.seq <= p.seq { throw HistoryTransferError("seq \(m.seq) after \(p.seq)") }
            case nil:
                throw HistoryTransferError("archive does not start with a header")
            default:
                throw HistoryTransferError("message before any conversation")
            }
        }
    }
}
