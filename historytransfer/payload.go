// Package historytransfer moves a phone's history to a desktop through the
// hub, sealed to a key only the desktop holds. It carries the QR payload, the
// binding tag, chunk sealing and the archive stream. See
// docs/specs/history-transfer.md.
package historytransfer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	payloadPrefix  = "sock-history"
	payloadVersion = "1"
)

var b64 = base64.RawURLEncoding.Strict()

// hostAliases maps every alias of a hub host to its one name.
var hostAliases = map[string]string{
	"api.sock.aeryx.ai": "api.joinsock.com",
}

// Payload is what the desktop shows as a QR code and the phone scans.
type Payload struct {
	Secret    [SecretLen]byte
	KeyDigest [32]byte
	// Host is the hub's API host as the desktop is configured, port included
	// when there is one.
	Host string
}

// String encodes p as sock-history:1:<secret>:<key digest>:<host>.
func (p Payload) String() string {
	return strings.Join([]string{payloadPrefix, payloadVersion,
		b64.EncodeToString(p.Secret[:]), b64.EncodeToString(p.KeyDigest[:]), p.Host}, ":")
}

// ParsePayload decodes a scanned payload. The host is everything after the
// fourth colon, so a host:port parses.
func ParsePayload(s string) (Payload, error) {
	var p Payload
	// The decoder skips CR and LF; a code holding either is not one we wrote.
	if strings.ContainsAny(s, "\r\n") {
		return p, errors.New("historytransfer: line break in code")
	}
	parts := strings.SplitN(s, ":", 5)
	if len(parts) != 5 || parts[0] != payloadPrefix {
		return p, errors.New("historytransfer: not a history transfer code")
	}
	if parts[1] != payloadVersion {
		return p, fmt.Errorf("historytransfer: unsupported code version %q", parts[1])
	}
	if err := decode32(p.Secret[:], parts[2]); err != nil {
		return p, fmt.Errorf("historytransfer: secret: %w", err)
	}
	if err := decode32(p.KeyDigest[:], parts[3]); err != nil {
		return p, fmt.Errorf("historytransfer: key digest: %w", err)
	}
	if parts[4] == "" {
		return p, errors.New("historytransfer: empty host")
	}
	p.Host = parts[4]
	return p, nil
}

func decode32(dst []byte, s string) error {
	b, err := b64.DecodeString(s)
	if err != nil {
		return err
	}
	if len(b) != len(dst) {
		return fmt.Errorf("%d bytes, want %d", len(b), len(dst))
	}
	copy(dst, b)
	return nil
}

// SameHost reports whether a and b name the same hub once aliases are mapped
// on the host name. Ports must match; a missing port is 443.
func SameHost(a, b string) bool {
	return canonicalHost(a) == canonicalHost(b)
}

func canonicalHost(h string) string {
	name, port, err := net.SplitHostPort(h)
	if err != nil {
		name, port = h, "443"
	}
	name = strings.ToLower(name)
	if c, ok := hostAliases[name]; ok {
		name = c
	}
	return net.JoinHostPort(name, port)
}
