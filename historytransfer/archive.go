package historytransfer

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protodelim"

	sockv1 "github.com/aeryx-ai/sock-contract/gen/go/sock/v1"
)

// FormatVersion is the archive format this build writes and the highest it
// reads.
const FormatVersion = 1

// MaxRecordBytes caps one archive record; the reader refuses a longer one.
// Swift's maxRecordBytes is the same.
const MaxRecordBytes = 4 << 20

// ArchiveWriter writes HistoryRecords, each prefixed by its varint length.
type ArchiveWriter struct{ w io.Writer }

func NewArchiveWriter(w io.Writer) *ArchiveWriter { return &ArchiveWriter{w: w} }

func (a *ArchiveWriter) Write(r *sockv1.HistoryRecord) error {
	if _, err := protodelim.MarshalTo(a.w, r); err != nil {
		return fmt.Errorf("historytransfer: write record: %w", err)
	}
	return nil
}

// ArchiveReader reads the records an ArchiveWriter wrote.
type ArchiveReader struct{ r *bufio.Reader }

func NewArchiveReader(r io.Reader) *ArchiveReader { return &ArchiveReader{r: bufio.NewReader(r)} }

// Next returns the next record, or io.EOF after the last one. A stream cut
// inside a record is an error, not io.EOF.
func (a *ArchiveReader) Next() (*sockv1.HistoryRecord, error) {
	rec := &sockv1.HistoryRecord{}
	if err := (protodelim.UnmarshalOptions{MaxSize: MaxRecordBytes}).UnmarshalFrom(a.r, rec); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("historytransfer: read record: %w", err)
	}
	return rec, nil
}

// OrderValidator checks an archive's record order, one record at a time: one
// header first, then accounts, then each conversation once, followed by its
// messages in rising seq from 1 up. The zero value is ready for the first
// record. A refused record leaves the validator where it was.
type OrderValidator struct {
	prev *sockv1.HistoryRecord
	seen map[string]bool
}

// Check reports whether next may follow the records already checked.
func (v *OrderValidator) Check(next *sockv1.HistoryRecord) error {
	if err := v.check(next); err != nil {
		return err
	}
	if c := next.GetConversation(); c != nil {
		if v.seen == nil {
			v.seen = map[string]bool{}
		}
		v.seen[c.GetId()] = true
	}
	v.prev = next
	return nil
}

func (v *OrderValidator) check(next *sockv1.HistoryRecord) error {
	prev := v.prev
	switch r := next.GetRecord().(type) {
	case *sockv1.HistoryRecord_Header:
		if prev != nil {
			return errors.New("historytransfer: header after the first record")
		}
		if ver := r.Header.GetFormatVersion(); ver == 0 || ver > FormatVersion {
			return fmt.Errorf("historytransfer: archive format %d, this build reads 1 to %d", ver, FormatVersion)
		}
		if r.Header.GetAccountId() == "" {
			return errors.New("historytransfer: header has no account id")
		}
		return nil
	case nil:
		return errors.New("historytransfer: empty record")
	}
	if prev == nil {
		return errors.New("historytransfer: archive does not start with a header")
	}
	switch r := next.GetRecord().(type) {
	case *sockv1.HistoryRecord_Account:
		switch prev.GetRecord().(type) {
		case *sockv1.HistoryRecord_Header, *sockv1.HistoryRecord_Account:
			return nil
		}
		return errors.New("historytransfer: account after a conversation")
	case *sockv1.HistoryRecord_Conversation:
		if v.seen[r.Conversation.GetId()] {
			return fmt.Errorf("historytransfer: conversation %q repeated", r.Conversation.GetId())
		}
		return nil
	case *sockv1.HistoryRecord_Message:
		m := r.Message
		switch p := prev.GetRecord().(type) {
		case *sockv1.HistoryRecord_Conversation:
			if m.GetConversationId() != p.Conversation.GetId() {
				return errors.New("historytransfer: message outside the current conversation")
			}
			if m.GetSeq() <= 0 {
				return fmt.Errorf("historytransfer: first message seq %d, want 1 or more", m.GetSeq())
			}
			return nil
		case *sockv1.HistoryRecord_Message:
			if m.GetConversationId() != p.Message.GetConversationId() {
				return errors.New("historytransfer: message outside the current conversation")
			}
			if m.GetSeq() <= p.Message.GetSeq() {
				return fmt.Errorf("historytransfer: seq %d after %d", m.GetSeq(), p.Message.GetSeq())
			}
			return nil
		}
		return errors.New("historytransfer: message before any conversation")
	}
	return fmt.Errorf("historytransfer: unknown record %T", next.GetRecord())
}
