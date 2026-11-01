package historytransfer

import (
	"bytes"
	"crypto/hpke"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	sockv1 "github.com/aeryx-ai/sock-contract/gen/go/sock/v1"
)

var update = flag.Bool("update", false, "rewrite the checked-in test vector")

func seq32() [32]byte {
	var b [32]byte
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func TestParsePayloadRoundTrip(t *testing.T) {
	for _, host := range []string{"api.joinsock.com", "staging-api.joinsock.com:8443", "localhost:8080"} {
		want := Payload{Secret: seq32(), Host: host}
		want.KeyDigest[0], want.KeyDigest[31] = 0xff, 0xee
		s := want.String()
		if !strings.HasPrefix(s, "sock-history:1:") || strings.Contains(s, "=") {
			t.Fatalf("String() = %q", s)
		}
		got, err := ParsePayload(s)
		if err != nil {
			t.Fatalf("ParsePayload(%q): %v", s, err)
		}
		if got != want {
			t.Fatalf("round trip: got %+v, want %+v", got, want)
		}
	}
}

func TestParsePayloadRejects(t *testing.T) {
	good := Payload{Secret: seq32(), KeyDigest: seq32(), Host: "api.joinsock.com"}.String()
	parts := strings.SplitN(good, ":", 5)
	b64 := func(n int) string { return base64.RawURLEncoding.EncodeToString(make([]byte, n)) }
	cases := map[string]string{
		"wrong prefix":      "sock-hist:1:" + strings.Join(parts[2:], ":"),
		"wrong version":     "sock-history:2:" + strings.Join(parts[2:], ":"),
		"31-byte secret":    "sock-history:1:" + b64(31) + ":" + parts[3] + ":" + parts[4],
		"33-byte digest":    "sock-history:1:" + parts[2] + ":" + b64(33) + ":" + parts[4],
		"padded secret":     "sock-history:1:" + parts[2] + "=:" + parts[3] + ":" + parts[4],
		"std alphabet":      "sock-history:1:" + base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 16)) + ":" + parts[3] + ":" + parts[4],
		"LF in secret":      "sock-history:1:" + parts[2][:10] + "\n" + parts[2][10:] + ":" + parts[3] + ":" + parts[4],
		"CR in digest":      "sock-history:1:" + parts[2] + ":" + parts[3][:10] + "\r" + parts[3][10:] + ":" + parts[4],
		"CRLF in host":      good + "\r\n",
		"empty host":        "sock-history:1:" + parts[2] + ":" + parts[3] + ":",
		"missing host":      "sock-history:1:" + parts[2] + ":" + parts[3],
		"empty":             "",
		"non-canonical b64": "sock-history:1:" + parts[2][:42] + "B:" + parts[3] + ":" + parts[4],
	}
	url := "sock-history:1:" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 16)) + ":" + parts[3] + ":" + parts[4]
	if !strings.ContainsAny(url, "-_") {
		t.Fatal("alphabet case does not exercise - or _")
	}
	if _, err := ParsePayload(url); err != nil {
		t.Fatalf("url alphabet refused: %v", err)
	}
	for name, s := range cases {
		if _, err := ParsePayload(s); err == nil {
			t.Errorf("%s: ParsePayload(%q) accepted", name, s)
		}
	}
}

func TestParsePayloadHostAlias(t *testing.T) {
	if !SameHost("api.sock.aeryx.ai", "api.joinsock.com") {
		t.Error("old alias is not the production host")
	}
	if !SameHost("api.joinsock.com", "API.JoinSock.com") {
		t.Error("host comparison is case sensitive")
	}
	if !SameHost("api.sock.aeryx.ai:443", "api.joinsock.com") {
		t.Error("alias with the default port is not the production host")
	}
	if !SameHost("api.sock.aeryx.ai:8443", "api.joinsock.com:8443") {
		t.Error("alias with a port is not the production host on that port")
	}
	if SameHost("api.sock.aeryx.ai:8443", "api.joinsock.com") {
		t.Error("different ports match")
	}
	if SameHost("staging-api.joinsock.com", "api.joinsock.com") {
		t.Error("staging matches production")
	}
	if SameHost("staging-api.joinsock.com", "api.sock.aeryx.ai") {
		t.Error("staging matches the production alias")
	}
}

func TestTagKnownAnswer(t *testing.T) {
	// Expected value from:
	//   printf 'sock history transfer v1' > msg
	//   printf '\x6b\xa7\xb8\x10\x9d\xad\x11\xd1\x80\xb4\x00\xc0\x4f\xd4\x30\xc8' >> msg
	//   openssl dgst -sha256 -mac HMAC -macopt hexkey:000102...1e1f msg
	const want = "7c3d0918b3d310a747516eb4e6fd02a66f78641fcdcc0b03a229f02ea52ae0d5"
	tag := Tag(seq32(), uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"))
	if got := hex.EncodeToString(tag[:]); got != want {
		t.Fatalf("Tag = %s, want %s", got, want)
	}
}

func newPair(t *testing.T, tag [32]byte) (*Sealer, *Opener) {
	t.Helper()
	priv, pub, err := NewRecipientKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(pub) != RecipientKeyLen {
		t.Fatalf("public key is %d bytes", len(pub))
	}
	enc, s, err := NewSealer(pub, tag)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) != EncLen {
		t.Fatalf("enc is %d bytes", len(enc))
	}
	o, err := NewOpener(priv, enc, tag)
	if err != nil {
		t.Fatal(err)
	}
	return s, o
}

func TestSealOpenRoundTrip(t *testing.T) {
	s, o := newPair(t, seq32())
	chunks := [][]byte{[]byte("one"), bytes.Repeat([]byte{7}, MaxChunkPlaintext), []byte("three")}
	for i, pt := range chunks {
		final := i == len(chunks)-1
		ct, err := s.Seal(uint32(i), final, pt)
		if err != nil {
			t.Fatalf("Seal %d: %v", i, err)
		}
		got, err := o.Open(uint32(i), final, ct)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("chunk %d differs", i)
		}
	}
	if _, err := s.Seal(3, true, []byte("x")); err == nil {
		t.Error("Seal after final accepted")
	}
	if _, err := o.Open(3, true, []byte("x")); err == nil {
		t.Error("Open after final accepted")
	}
}

func TestSealRefuses(t *testing.T) {
	s, _ := newPair(t, seq32())
	if _, err := s.Seal(1, false, []byte("x")); err == nil {
		t.Error("Seal out of order accepted")
	}
	if _, err := s.Seal(0, false, nil); err == nil {
		t.Error("Seal of an empty chunk accepted")
	}
	if _, err := s.Seal(0, false, make([]byte, MaxChunkPlaintext+1)); err == nil {
		t.Error("Seal of an oversized chunk accepted")
	}
	for i := range MaxChunks - 1 {
		if _, err := s.Seal(uint32(i), false, []byte("x")); err != nil {
			t.Fatalf("Seal %d: %v", i, err)
		}
	}
	if _, err := s.Seal(MaxChunks-1, false, []byte("x")); err == nil {
		t.Error("last allowed chunk without final accepted")
	}
	if _, err := s.Seal(MaxChunks-1, true, []byte("x")); err != nil {
		t.Errorf("last allowed chunk with final: %v", err)
	}
	if _, _, err := NewSealer(make([]byte, RecipientKeyLen-1), seq32()); err == nil {
		t.Error("short recipient key accepted")
	}
}

func TestOpenRejectsWrongIndex(t *testing.T) {
	s, o := newPair(t, seq32())
	ct, err := s.Seal(0, false, []byte("zero"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Open(1, false, ct); err == nil {
		t.Fatal("Open at the wrong index accepted")
	}
	if _, err := o.Open(0, false, ct); err != nil {
		t.Fatalf("Open at the right index after a refusal: %v", err)
	}
}

func TestOpenRejectsFinalFlip(t *testing.T) {
	s, o := newPair(t, seq32())
	ct, err := s.Seal(0, false, []byte("zero"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Open(0, true, ct); err == nil {
		t.Fatal("Open with the final flag flipped accepted")
	}
	if _, err := o.Open(0, false, ct); err == nil {
		t.Fatal("Open after an authentication failure accepted")
	}
	if o.Done() {
		t.Fatal("failed opener reports done")
	}
}

func TestOpenerDone(t *testing.T) {
	s, o := newPair(t, seq32())
	for i := range 2 {
		final := i == 1
		ct, err := s.Seal(uint32(i), final, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if o.Done() {
			t.Fatalf("done before chunk %d", i)
		}
		if _, err := o.Open(uint32(i), final, ct); err != nil {
			t.Fatal(err)
		}
	}
	if !o.Done() {
		t.Fatal("not done after the final chunk")
	}
}

func TestOpenRejectsOtherTag(t *testing.T) {
	priv, pub, err := NewRecipientKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, s, err := NewSealer(pub, seq32())
	if err != nil {
		t.Fatal(err)
	}
	ct, err := s.Seal(0, true, []byte("zero"))
	if err != nil {
		t.Fatal(err)
	}
	other := seq32()
	other[0] ^= 1
	o, err := NewOpener(priv, enc, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Open(0, true, ct); err == nil {
		t.Fatal("Open under another tag accepted")
	}
}

func TestKeyDigest(t *testing.T) {
	_, pub, err := NewRecipientKey()
	if err != nil {
		t.Fatal(err)
	}
	d := KeyDigest(pub)
	pub[0] ^= 1
	if KeyDigest(pub) == d {
		t.Fatal("digest ignores the key")
	}
}

func sampleArchive(n int) []*sockv1.HistoryRecord {
	recs := []*sockv1.HistoryRecord{
		{Record: &sockv1.HistoryRecord_Header{Header: &sockv1.HistoryHeader{
			FormatVersion: FormatVersion, AccountId: "acct-1", ExportedAt: timestamppb.New(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)),
		}}},
		{Record: &sockv1.HistoryRecord_Account{Account: &sockv1.Account{Id: "acct-1", DisplayName: "Guy"}}},
		{Record: &sockv1.HistoryRecord_Account{Account: &sockv1.Account{Id: "acct-2", DisplayName: "Puppet"}}},
	}
	conv := 0
	for i := range n {
		if i%100 == 0 {
			conv++
			recs = append(recs, &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Conversation{
				Conversation: &sockv1.Conversation{Id: fmt.Sprintf("conv-%d", conv), MemberIds: []string{"acct-1", "acct-2"}, ReadThroughSeq: 3},
			}})
		}
		recs = append(recs, &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Message{
			Message: &sockv1.Message{Id: fmt.Sprintf("m-%d", i), ConversationId: fmt.Sprintf("conv-%d", conv), Seq: int64(i%100 + 1), SenderId: "acct-2"},
		}})
	}
	return recs
}

func TestArchiveRoundTrip(t *testing.T) {
	recs := sampleArchive(1000 - 3 - 10)
	if len(recs) != 1000 {
		t.Fatalf("built %d records", len(recs))
	}
	var buf bytes.Buffer
	w := NewArchiveWriter(&buf)
	for _, r := range recs {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	pr, pw := io.Pipe()
	t.Cleanup(func() { pr.CloseWithError(io.ErrClosedPipe) })
	go func() {
		b := buf.Bytes()
		for len(b) > 0 {
			n := min(7, len(b))
			if _, err := pw.Write(b[:n]); err != nil {
				return
			}
			b = b[n:]
		}
		pw.Close()
	}()
	r := NewArchiveReader(pr)
	var order OrderValidator
	for i, want := range recs {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if !proto.Equal(got, want) {
			t.Fatalf("record %d: got %v, want %v", i, got, want)
		}
		if err := order.Check(got); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after the last record: %v, want io.EOF", err)
	}
}

func TestArchiveReaderTruncated(t *testing.T) {
	var buf bytes.Buffer
	if err := NewArchiveWriter(&buf).Write(sampleArchive(0)[0]); err != nil {
		t.Fatal(err)
	}
	r := NewArchiveReader(bytes.NewReader(buf.Bytes()[:buf.Len()-1]))
	if _, err := r.Next(); err == nil || err == io.EOF {
		t.Fatalf("truncated record: %v, want a non-EOF error", err)
	}
}

func TestOrderValidator(t *testing.T) {
	type rec = *sockv1.HistoryRecord
	hdrAs := func(v uint32, id string) rec {
		return &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Header{Header: &sockv1.HistoryHeader{FormatVersion: v, AccountId: id}}}
	}
	hdr := hdrAs(1, "a")
	acct := &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Account{Account: &sockv1.Account{Id: "a"}}}
	conv := func(id string) rec {
		return &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Conversation{Conversation: &sockv1.Conversation{Id: id}}}
	}
	msg := func(c string, seq int64) rec {
		return &sockv1.HistoryRecord{Record: &sockv1.HistoryRecord_Message{Message: &sockv1.Message{ConversationId: c, Seq: seq}}}
	}
	// Each sequence is fed to a fresh validator; ok ones pass whole, bad ones
	// pass all but the last record and refuse the last.
	ok := map[string][]rec{
		"header only":                           {hdr},
		"accounts after header":                 {hdr, acct, acct},
		"conversation after header":             {hdr, conv("c")},
		"conversation after account":            {hdr, acct, conv("c")},
		"conversation after empty conversation": {hdr, conv("c"), conv("d")},
		"messages in rising seq":                {hdr, conv("c"), msg("c", 1), msg("c", 5)},
		"conversation after message":            {hdr, conv("c"), msg("c", 1), conv("d"), msg("d", 1)},
	}
	for name, seq := range ok {
		var v OrderValidator
		for i, r := range seq {
			if err := v.Check(r); err != nil {
				t.Errorf("%s: record %d: %v", name, i, err)
			}
		}
	}
	bad := map[string][]rec{
		"account first":                   {acct},
		"newer format":                    {hdrAs(FormatVersion+1, "a")},
		"unset format":                    {hdrAs(0, "a")},
		"empty account id":                {hdrAs(1, "")},
		"header after header":             {hdr, hdr},
		"second header":                   {hdr, acct, hdr},
		"account after conversation":      {hdr, conv("c"), acct},
		"account after message":           {hdr, conv("c"), msg("c", 1), acct},
		"message after header":            {hdr, msg("c", 1)},
		"message after account":           {hdr, acct, msg("c", 1)},
		"message of another conversation": {hdr, conv("c"), msg("d", 1)},
		"message switching conversation":  {hdr, conv("c"), msg("c", 1), msg("d", 2)},
		"first seq zero":                  {hdr, conv("c"), msg("c", 0)},
		"first seq negative":              {hdr, conv("c"), msg("c", -1)},
		"seq repeated":                    {hdr, conv("c"), msg("c", 2), msg("c", 2)},
		"seq falling":                     {hdr, conv("c"), msg("c", 2), msg("c", 1)},
		"conversation repeated":           {hdr, conv("c"), msg("c", 5), conv("d"), conv("c")},
		"empty record":                    {hdr, {}},
		"nil record":                      {hdr, nil},
	}
	for name, seq := range bad {
		var v OrderValidator
		last := len(seq) - 1
		for i, r := range seq[:last] {
			if err := v.Check(r); err != nil {
				t.Fatalf("%s: prefix record %d: %v", name, i, err)
			}
		}
		if err := v.Check(seq[last]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The vector Task 3's Swift test opens. Layout of go-sealed.bin, all integers
// unsigned big-endian:
//
//	secret      32 bytes   transfer secret
//	account     16 bytes   human id UUID bytes
//	tag         32 bytes   Tag(secret, account)
//	seed        32 bytes   X-Wing private key seed (hpke PrivateKey.Bytes)
//	public    1216 bytes   X-Wing public key from seed
//	enc       1120 bytes   HPKE encapsulated key
//	payloadLen  u32        then payloadLen bytes: Payload{secret, KeyDigest(public), host}.String()
//	chunkCount  u32        then chunkCount chunks, each:
//	  index     u32
//	  final     u8         0x01 final, 0x00 not
//	  ptLen     u32        then ptLen bytes of plaintext
//	  ctLen     u32        then ctLen bytes of ciphertext
//
// The chunk plaintexts concatenate to a delimited HistoryRecord archive (see
// sampleArchive) cut at an arbitrary byte, not at a record boundary.
const vectorPath = "../testdata/history-transfer/go-sealed.bin"

type vectorChunk struct {
	index     uint32
	final     bool
	plaintext []byte
	ct        []byte
}

type vector struct {
	secret  [32]byte
	account uuid.UUID
	tag     [32]byte
	seed    [32]byte
	public  []byte
	enc     []byte
	payload string
	chunks  []vectorChunk
}

func (v vector) marshal() []byte {
	var b []byte
	b = append(b, v.secret[:]...)
	b = append(b, v.account[:]...)
	b = append(b, v.tag[:]...)
	b = append(b, v.seed[:]...)
	b = append(b, v.public...)
	b = append(b, v.enc...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(v.payload)))
	b = append(b, v.payload...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(v.chunks)))
	for _, c := range v.chunks {
		b = binary.BigEndian.AppendUint32(b, c.index)
		if c.final {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
		b = binary.BigEndian.AppendUint32(b, uint32(len(c.plaintext)))
		b = append(b, c.plaintext...)
		b = binary.BigEndian.AppendUint32(b, uint32(len(c.ct)))
		b = append(b, c.ct...)
	}
	return b
}

func unmarshalVector(b []byte) (vector, error) {
	var v vector
	take := func(n int) ([]byte, error) {
		if len(b) < n {
			return nil, errors.New("vector truncated")
		}
		out := b[:n]
		b = b[n:]
		return out, nil
	}
	u32 := func() (uint32, error) {
		x, err := take(4)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint32(x), nil
	}
	fixed := []struct {
		dst []byte
		n   int
	}{{v.secret[:], 32}, {v.account[:], 16}, {v.tag[:], 32}, {v.seed[:], 32}}
	for _, f := range fixed {
		x, err := take(f.n)
		if err != nil {
			return v, err
		}
		copy(f.dst, x)
	}
	var err error
	if v.public, err = take(RecipientKeyLen); err != nil {
		return v, err
	}
	if v.enc, err = take(EncLen); err != nil {
		return v, err
	}
	n, err := u32()
	if err != nil {
		return v, err
	}
	p, err := take(int(n))
	if err != nil {
		return v, err
	}
	v.payload = string(p)
	count, err := u32()
	if err != nil {
		return v, err
	}
	for range count {
		var c vectorChunk
		if c.index, err = u32(); err != nil {
			return v, err
		}
		f, err := take(1)
		if err != nil {
			return v, err
		}
		c.final = f[0] == 1
		for _, dst := range []*[]byte{&c.plaintext, &c.ct} {
			n, err := u32()
			if err != nil {
				return v, err
			}
			if *dst, err = take(int(n)); err != nil {
				return v, err
			}
		}
		v.chunks = append(v.chunks, c)
	}
	if len(b) != 0 {
		return v, fmt.Errorf("vector has %d trailing bytes", len(b))
	}
	return v, nil
}

func buildVector(t *testing.T) vector {
	t.Helper()
	v := vector{secret: seq32(), account: uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")}
	v.tag = Tag(v.secret, v.account)
	for i := range v.seed {
		v.seed[i] = byte(0xa0 + i)
	}
	priv, err := hpke.MLKEM768X25519().NewPrivateKey(v.seed[:])
	if err != nil {
		t.Fatal(err)
	}
	v.public = priv.PublicKey().Bytes()
	v.payload = Payload{Secret: v.secret, KeyDigest: KeyDigest(v.public), Host: "staging-api.joinsock.com:8443"}.String()
	enc, s, err := NewSealer(v.public, v.tag)
	if err != nil {
		t.Fatal(err)
	}
	v.enc = enc
	var archive bytes.Buffer
	w := NewArchiveWriter(&archive)
	for _, r := range sampleArchive(5) {
		if err := w.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	cut := archive.Len()/2 + 1
	for i, pt := range [][]byte{archive.Bytes()[:cut], archive.Bytes()[cut:]} {
		c := vectorChunk{index: uint32(i), final: i == 1, plaintext: pt}
		if c.ct, err = s.Seal(c.index, c.final, pt); err != nil {
			t.Fatal(err)
		}
		v.chunks = append(v.chunks, c)
	}
	return v
}

func TestWriteGoVector(t *testing.T) {
	if *update {
		if err := os.MkdirAll(filepath.Dir(vectorPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorPath, buildVector(t).marshal(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatalf("read vector (run with -update to write it): %v", err)
	}
	v, err := unmarshalVector(b)
	if err != nil {
		t.Fatal(err)
	}
	if Tag(v.secret, v.account) != v.tag {
		t.Fatal("vector tag does not match Tag(secret, account)")
	}
	priv, err := hpke.MLKEM768X25519().NewPrivateKey(v.seed[:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priv.PublicKey().Bytes(), v.public) {
		t.Fatal("vector public key does not derive from its seed")
	}
	p, err := ParsePayload(v.payload)
	if err != nil {
		t.Fatal(err)
	}
	if p.Secret != v.secret || p.KeyDigest != KeyDigest(v.public) {
		t.Fatal("vector payload does not carry its secret and key digest")
	}
	o, err := NewOpener(priv, v.enc, v.tag)
	if err != nil {
		t.Fatal(err)
	}
	var archive []byte
	for _, c := range v.chunks {
		pt, err := o.Open(c.index, c.final, c.ct)
		if err != nil {
			t.Fatalf("open chunk %d: %v", c.index, err)
		}
		if !bytes.Equal(pt, c.plaintext) {
			t.Fatalf("chunk %d plaintext differs", c.index)
		}
		archive = append(archive, pt...)
	}
	r := NewArchiveReader(bytes.NewReader(archive))
	var order OrderValidator
	count := 0
	for {
		rec, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := order.Check(rec); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if want := len(sampleArchive(5)); count != want {
		t.Fatalf("vector archive has %d records, want %d", count, want)
	}
}

// swift-sealed.bin has go-sealed.bin's layout. Swift's testWriteSwiftVector
// seals it to the Go vector's public key, so it opens with the Go vector's
// seed. Rewrite it with UPDATE_VECTORS=1 swift test --filter HistoryTransferTests.
const swiftVectorPath = "../testdata/history-transfer/swift-sealed.bin"

func TestOpenSwiftVector(t *testing.T) {
	read := func(path string) vector {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		v, err := unmarshalVector(b)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return v
	}
	gov, v := read(vectorPath), read(swiftVectorPath)
	if !bytes.Equal(v.public, gov.public) {
		t.Fatal("Swift vector is not sealed to the Go vector's key")
	}
	if Tag(v.secret, v.account) != v.tag {
		t.Fatal("Swift tag does not match Tag(secret, account)")
	}
	p, err := ParsePayload(v.payload)
	if err != nil {
		t.Fatal(err)
	}
	if p.Secret != v.secret || p.KeyDigest != KeyDigest(v.public) || p.Host != "staging-api.joinsock.com:8443" {
		t.Fatalf("Swift payload %q does not carry its secret, key digest and host", v.payload)
	}
	priv, err := hpke.MLKEM768X25519().NewPrivateKey(gov.seed[:])
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewOpener(priv, v.enc, v.tag)
	if err != nil {
		t.Fatal(err)
	}
	var archive []byte
	for _, c := range v.chunks {
		pt, err := o.Open(c.index, c.final, c.ct)
		if err != nil {
			t.Fatalf("open Swift chunk %d: %v", c.index, err)
		}
		if !bytes.Equal(pt, c.plaintext) {
			t.Fatalf("Swift chunk %d plaintext differs", c.index)
		}
		archive = append(archive, pt...)
	}
	if !o.Done() {
		t.Fatal("Swift vector has no final chunk")
	}
	r := NewArchiveReader(bytes.NewReader(archive))
	var order OrderValidator
	want := sampleArchive(5)
	for i := 0; ; i++ {
		rec, err := r.Next()
		if err == io.EOF {
			if i != len(want) {
				t.Fatalf("Swift archive has %d records, want %d", i, len(want))
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := order.Check(rec); err != nil {
			t.Fatal(err)
		}
		if i >= len(want) || !proto.Equal(rec, want[i]) {
			t.Fatalf("Swift record %d: %v", i, rec)
		}
	}
}
