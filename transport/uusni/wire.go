// Package uusni implements UU 11.3.5's custom SNI/TCP protocol without outer TLS.
package uusni

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

type Config struct {
	Server   string
	Port     int
	ObfsKey  int
	SNICrypt bool
}

func (c Config) Validate() error {
	ip, err := netip.ParseAddr(c.Server)
	if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.As4()[0] >= 224 {
		return errors.New("uusni: server must be a numeric unicast IPv4 address")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("uusni: port must be between 1 and 65535")
	}
	if c.ObfsKey < 0 || c.ObfsKey > 255 {
		return errors.New("uusni: obfs-key must be between 0 and 255")
	}
	return nil
}

func addressKey(c Config) [16]byte {
	var key [16]byte
	for i := range key {
		key[i] = 'U'
	}
	copy(key[:], c.Server+":"+strconv.Itoa(c.Port))
	return key
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	// Preserve a valid absolute DNS name, including its final dot, on the wire.
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := range label {
			b := label[i]
			if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-') {
				return false
			}
		}
	}
	return true
}

// Header retains the original target port and XORs the entire custom header.
// A successful TCP connect does not imply the target accepted the connection.
func Header(c Config, host string, port uint16) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if port == 0 || !validHost(host) {
		return nil, errors.New("uusni: a valid ASCII domain/IPv4 target and port are required")
	}
	field := []byte(host)
	flag := byte(0)
	if c.SNICrypt {
		if len(field) > 222 {
			return nil, errors.New("uusni: encrypted target exceeds the 222-byte address limit")
		}
		key := addressKey(c)
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCMWithNonceSize(block, 16)
		if err != nil {
			return nil, err
		}
		// Fixed zero IV, empty AAD and trailing tag match the captured protocol;
		// this is a compatibility requirement, not a new cryptographic design.
		iv := make([]byte, 16)
		field = aead.Seal(append([]byte{16}, iv...), iv, field, nil)
		flag = 1
	}
	if len(field) > 255 {
		return nil, errors.New("uusni: address field length overflow")
	}
	header := make([]byte, 7+len(field))
	copy(header, []byte{'U', 'U', 1, flag})
	binary.BigEndian.PutUint16(header[4:6], port)
	header[6] = byte(len(field))
	copy(header[7:], field)
	xor(header, byte(c.ObfsKey))
	return header, nil
}

func xor(b []byte, key byte) {
	for i := range b {
		b[i] ^= key
	}
}
