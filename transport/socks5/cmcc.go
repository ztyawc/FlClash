package socks5

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/metacubex/mihomo/common/buf"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/pool"
)

const (
	CMCCAuthMethod80 byte = 0x80
	CMCCAuthMethod82 byte = 0x82
)

var cmccAuthMethod82FixedData = [...]byte{
	0x14, 0x01, 0x01, 0x01, 0x02, 0x04, 0x00, 0x00, 0x00, 0x00,
	0x03, 0x02, 0x27, 0x10, 0x04, 0x01, 0x01, 0x05, 0x02, 0x00, 0x04,
}

// ClientHandshakeCMCC performs the private SOCKS5 authentication used by the
// CMCC education accelerator. rw must obfuscate every client-to-server write
// with XOR 0xff and leave reads untouched; NewCMCCConn provides those semantics.
func ClientHandshakeCMCC(rw io.ReadWriter, addr Addr, command Command, user *User, method byte) (Addr, error) {
	if err := validateCMCCUser(user); err != nil {
		return nil, err
	}
	if method != CMCCAuthMethod80 && method != CMCCAuthMethod82 {
		return nil, fmt.Errorf("unsupported CMCC SOCKS5 authentication method: 0x%02x", method)
	}
	if command != CmdConnect && command != CmdUDPAssociate {
		return nil, fmt.Errorf("unsupported CMCC SOCKS5 command: 0x%02x", command)
	}

	if err := writeFull(rw, []byte{Version, 1, method}); err != nil {
		return nil, err
	}

	var challenge []byte
	switch method {
	case CMCCAuthMethod80:
		response := make([]byte, 2)
		if _, err := io.ReadFull(rw, response); err != nil {
			return nil, fmt.Errorf("read CMCC 0x80 challenge: %w", err)
		}
		if response[0] != Version {
			return nil, fmt.Errorf("unexpected CMCC SOCKS5 version: 0x%02x", response[0])
		}
		challenge = response[1:2]
	case CMCCAuthMethod82:
		response := make([]byte, 6)
		if _, err := io.ReadFull(rw, response); err != nil {
			return nil, fmt.Errorf("read CMCC 0x82 challenge: %w", err)
		}
		if response[0] != Version || response[1] != method {
			return nil, fmt.Errorf("unexpected CMCC 0x82 method response: %x", response[:2])
		}
		challenge = response[2:6]
	}

	authRequest := buildCMCCAuthRequest(user, method, challenge)
	if err := writeFull(rw, authRequest); err != nil {
		return nil, err
	}

	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(rw, authResponse); err != nil {
		return nil, fmt.Errorf("read CMCC authentication response: %w", err)
	}
	if authResponse[0] != 1 || authResponse[1] != 0 {
		return nil, fmt.Errorf("CMCC authentication rejected: version=0x%02x status=0x%02x", authResponse[0], authResponse[1])
	}

	request := make([]byte, 3+len(addr))
	request[0] = Version
	request[1] = command
	copy(request[3:], addr)
	if err := writeFull(rw, request); err != nil {
		return nil, err
	}

	response := make([]byte, MaxAddrLen)
	if _, err := io.ReadFull(rw, response[:3]); err != nil {
		return nil, fmt.Errorf("read CMCC SOCKS5 response: %w", err)
	}
	if response[0] != Version {
		return nil, fmt.Errorf("unexpected CMCC SOCKS5 response version: 0x%02x", response[0])
	}
	if response[1] != 0 {
		return nil, Error(response[1])
	}
	if response[2] != 0 {
		return nil, fmt.Errorf("unexpected CMCC SOCKS5 reserved byte: 0x%02x", response[2])
	}

	return ReadAddr(rw, response)
}

func validateCMCCUser(user *User) error {
	if user == nil {
		return ErrAuth
	}
	if len(user.Username) == 0 {
		return errors.New("CMCC SOCKS5 username is required")
	}
	if len(user.Username) > MaxAuthLen {
		return fmt.Errorf("CMCC SOCKS5 username is too long: %d bytes", len(user.Username))
	}
	if len(user.Password) == 0 {
		return errors.New("CMCC SOCKS5 password is required")
	}
	return nil
}

func buildCMCCAuthRequest(user *User, method byte, challenge []byte) []byte {
	key := user.Username + user.Password
	requestLen := 1 + 1 + len(user.Username) + 1 + sha256.Size
	if method == CMCCAuthMethod82 {
		passwordMD5 := md5.Sum([]byte(user.Password))
		key = user.Username + hex.EncodeToString(passwordMD5[:])
		requestLen += len(cmccAuthMethod82FixedData)
	}

	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(challenge)

	request := make([]byte, 0, requestLen)
	request = append(request, 1, byte(len(user.Username)))
	request = append(request, user.Username...)
	request = append(request, sha256.Size)
	request = mac.Sum(request)
	if method == CMCCAuthMethod82 {
		request = append(request, cmccAuthMethod82FixedData[:]...)
	}
	return request
}

func writeFull(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := w.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func xorFF(dst, src []byte) {
	for i, value := range src {
		dst[i] = value ^ 0xff
	}
}

type cmccConn struct {
	N.ExtendedConn
}

// NewCMCCConn wraps a stream so only writes are XOR-obfuscated. Server reads
// remain plain, matching the protocol's intentionally asymmetric transport.
func NewCMCCConn(conn net.Conn) N.ExtendedConn {
	if wrapped, ok := conn.(*cmccConn); ok {
		return wrapped
	}
	return &cmccConn{ExtendedConn: N.NewExtendedConn(conn)}
}

func (c *cmccConn) Write(payload []byte) (int, error) {
	packet := pool.Get(len(payload))
	defer func() { _ = pool.Put(packet) }()
	xorFF(packet, payload)
	return c.ExtendedConn.Write(packet)
}

func (c *cmccConn) WriteBuffer(buffer *buf.Buffer) error {
	payload := buffer.Bytes()
	xorFF(payload, payload)
	return c.ExtendedConn.WriteBuffer(buffer)
}

func (c *cmccConn) Upstream() any {
	return c.ExtendedConn
}

func (c *cmccConn) ReaderReplaceable() bool {
	return true
}

func (c *cmccConn) WriterReplaceable() bool {
	return false
}

type cmccPacketConn struct {
	net.PacketConn
}

// NewCMCCPacketConn applies XOR to the complete outgoing SOCKS5 UDP datagram.
// Incoming datagrams are intentionally passed through unchanged.
func NewCMCCPacketConn(conn net.PacketConn) net.PacketConn {
	if wrapped, ok := conn.(*cmccPacketConn); ok {
		return wrapped
	}
	return &cmccPacketConn{PacketConn: conn}
}

func (c *cmccPacketConn) WriteTo(payload []byte, addr net.Addr) (int, error) {
	packet := pool.Get(len(payload))
	defer func() { _ = pool.Put(packet) }()
	xorFF(packet, payload)
	return c.PacketConn.WriteTo(packet, addr)
}

var _ N.ExtendedConn = (*cmccConn)(nil)
var _ net.PacketConn = (*cmccPacketConn)(nil)
