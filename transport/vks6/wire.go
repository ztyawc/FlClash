// Package vks6 implements the observed IPv4/TCP download-node protocol used
// by VK Android 4.4.16. It does not implement the game's UDP transport.
package vks6

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"unicode/utf8"
)

var authKey = [...]byte{0x3c, 0x26, 0x44, 0x63, 0x4c, 0x2b, 0x43, 0x40}

type Config struct {
	Server   string
	Port     int
	PortEnd  int
	Username string
	Password string
}

func (c Config) Validate() error {
	ip, err := netip.ParseAddr(c.Server)
	if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
		return errors.New("vks6: server must be a numeric unicast IPv4 address")
	}
	if c.Port < 1 || c.Port > c.PortEnd || c.PortEnd > 65535 {
		return errors.New("vks6: invalid node TCP port range")
	}
	if !utf8.ValidString(c.Username) || !utf8.ValidString(c.Password) ||
		len(c.Username) < 1 || len(c.Username) > 255 || len(c.Password) < 1 || len(c.Password) > 255 {
		return errors.New("vks6: username and password must contain 1-255 UTF-8 bytes")
	}
	return nil
}

// Header builds the complete request seen on the download TCP connection.
// The destination is encoded as IPv4 plus a network-order TCP port.
func Header(c Config, target netip.AddrPort) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	ip := target.Addr().Unmap()
	if !ip.Is4() || target.Port() == 0 {
		return nil, errors.New("vks6: target must be IPv4 with a nonzero port")
	}
	user := []byte(c.Username)
	pass := []byte(c.Password)
	request := make([]byte, 1+1+len(user)+1+len(pass)+2+4+2)
	i := 0
	request[i] = 0x06
	i++
	request[i] = byte(len(user))
	i++
	for j, b := range user {
		request[i+j] = b ^ authKey[j%len(authKey)]
	}
	i += len(user)
	request[i] = byte(len(pass))
	i++
	for j, b := range pass {
		request[i+j] = b ^ authKey[j%len(authKey)]
	}
	i += len(pass)
	request[i], request[i+1] = 0x09, 0x01
	i += 2
	addr := ip.As4()
	copy(request[i:i+4], addr[:])
	i += 4
	binary.BigEndian.PutUint16(request[i:i+2], target.Port())
	return request, nil
}
