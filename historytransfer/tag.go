package historytransfer

import (
	"crypto/hmac"
	"crypto/sha256"

	"github.com/google/uuid"
)

const (
	// Label prefixes the binding tag message and the HPKE info.
	Label = "sock history transfer v1"
	// SecretLen is the transfer secret's length in bytes.
	SecretLen = 32
)

// Tag is HMAC-SHA-256(key = secret, msg = Label ‖ account UUID bytes). The hub
// looks a transfer up by it; both devices bind ciphertext to it.
func Tag(secret [SecretLen]byte, account uuid.UUID) [32]byte {
	m := hmac.New(sha256.New, secret[:])
	m.Write([]byte(Label))
	m.Write(account[:])
	var t [32]byte
	m.Sum(t[:0])
	return t
}
