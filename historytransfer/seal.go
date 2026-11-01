package historytransfer

import (
	"crypto/hpke"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// RecipientKeyLen is the X-Wing public key length.
	RecipientKeyLen = 1216
	// EncLen is the X-Wing encapsulated key length.
	EncLen = 1120
	// MaxChunkPlaintext caps one chunk's plaintext.
	MaxChunkPlaintext = 1 << 20
	// MaxChunks caps a transfer's chunk count.
	MaxChunks = 128
)

// The suite: HPKE base mode, X-Wing, HKDF-SHA256, AES-256-GCM.
var (
	kem  = hpke.MLKEM768X25519()
	kdf  = hpke.HKDFSHA256()
	aead = hpke.AES256GCM()
)

// NewRecipientKey generates the desktop's key pair. The private key's Bytes is
// its 32-byte X-Wing seed.
func NewRecipientKey() (hpke.PrivateKey, []byte, error) {
	k, err := kem.GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("historytransfer: generate key: %w", err)
	}
	return k, k.PublicKey().Bytes(), nil
}

// KeyDigest is the SHA-256 of a recipient key, carried in the QR payload.
func KeyDigest(pub []byte) [32]byte {
	return sha256.Sum256(pub)
}

func info(tag [32]byte) []byte {
	return append([]byte(Label), tag[:]...)
}

// aad is uint32_be(index) ‖ final byte.
func aad(index uint32, final bool) []byte {
	b := binary.BigEndian.AppendUint32(make([]byte, 0, 5), index)
	if final {
		return append(b, 1)
	}
	return append(b, 0)
}

// chunkOrder is the bookkeeping Sealer and Opener share: chunks go in index
// order from 0 and nothing follows the final one.
type chunkOrder struct {
	next uint32
	done bool
}

func (c *chunkOrder) check(index uint32, final bool) error {
	switch {
	case c.done:
		return errors.New("historytransfer: chunk after the final one")
	case index != c.next:
		return fmt.Errorf("historytransfer: chunk %d, want %d", index, c.next)
	case index >= MaxChunks:
		return fmt.Errorf("historytransfer: chunk %d past the limit of %d", index, MaxChunks)
	case index == MaxChunks-1 && !final:
		return fmt.Errorf("historytransfer: chunk %d is the last allowed and not final", index)
	}
	return nil
}

func (c *chunkOrder) advance(final bool) {
	c.next++
	c.done = final
}

// Sealer seals an archive's chunks for one recipient key. The HPKE sequence
// number supplies each chunk's nonce, so chunks are sealed in index order.
type Sealer struct {
	s *hpke.Sender
	chunkOrder
}

// NewSealer starts a sending context to pub, bound to tag. enc goes to the hub
// in ClaimHistoryTransfer.
func NewSealer(pub []byte, tag [32]byte) ([]byte, *Sealer, error) {
	if len(pub) != RecipientKeyLen {
		return nil, nil, fmt.Errorf("historytransfer: recipient key is %d bytes, want %d", len(pub), RecipientKeyLen)
	}
	pk, err := kem.NewPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("historytransfer: recipient key: %w", err)
	}
	enc, s, err := hpke.NewSender(pk, kdf, aead, info(tag))
	if err != nil {
		return nil, nil, fmt.Errorf("historytransfer: sender: %w", err)
	}
	return enc, &Sealer{s: s}, nil
}

// Seal seals chunk index. It refuses an index that is not the next one, a
// chunk after the final one, an empty or oversized plaintext and a last
// allowed chunk that is not final.
func (s *Sealer) Seal(index uint32, final bool, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 || len(plaintext) > MaxChunkPlaintext {
		return nil, fmt.Errorf("historytransfer: chunk plaintext is %d bytes, want 1 to %d", len(plaintext), MaxChunkPlaintext)
	}
	if err := s.check(index, final); err != nil {
		return nil, err
	}
	ct, err := s.s.Seal(aad(index, final), plaintext)
	if err != nil {
		return nil, fmt.Errorf("historytransfer: seal chunk %d: %w", index, err)
	}
	s.advance(final)
	return ct, nil
}

// Opener opens an archive's chunks with the desktop's private key.
type Opener struct {
	r      *hpke.Recipient
	failed bool
	chunkOrder
}

// NewOpener starts a receiving context from the phone's enc, bound to tag.
func NewOpener(priv hpke.PrivateKey, enc []byte, tag [32]byte) (*Opener, error) {
	if len(enc) != EncLen {
		return nil, fmt.Errorf("historytransfer: encapsulated key is %d bytes, want %d", len(enc), EncLen)
	}
	r, err := hpke.NewRecipient(enc, priv, kdf, aead, info(tag))
	if err != nil {
		return nil, fmt.Errorf("historytransfer: recipient: %w", err)
	}
	return &Opener{r: r}, nil
}

// Open opens chunk index. It refuses an index that is not the next one and a
// chunk after the final one, leaving the context where it was. A chunk that
// fails authentication ends the opener: every later Open is refused.
func (o *Opener) Open(index uint32, final bool, ct []byte) ([]byte, error) {
	if o.failed {
		return nil, errors.New("historytransfer: opener stopped after a chunk failed authentication")
	}
	if err := o.check(index, final); err != nil {
		return nil, err
	}
	pt, err := o.r.Open(aad(index, final), ct)
	if err != nil {
		o.failed = true
		return nil, fmt.Errorf("historytransfer: open chunk %d: %w", index, err)
	}
	o.advance(final)
	return pt, nil
}

// Done reports whether the final chunk has opened. A stream that ends before
// Done is truncated and must be refused.
func (o *Opener) Done() bool { return o.done }
