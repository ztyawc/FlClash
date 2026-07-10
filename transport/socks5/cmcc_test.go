package socks5

import (
	"bytes"
	"encoding/hex"
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/buf"

	"github.com/stretchr/testify/require"
)

const (
	testCMCCUsername = "1234567899876543210"
	testCMCCPassword = "pAssWord"
)

type memoryConn struct {
	reader *bytes.Reader
	writes bytes.Buffer
}

func newMemoryConn(response []byte) *memoryConn {
	return &memoryConn{reader: bytes.NewReader(response)}
}

func (c *memoryConn) Read(payload []byte) (int, error)  { return c.reader.Read(payload) }
func (c *memoryConn) Write(payload []byte) (int, error) { return c.writes.Write(payload) }
func (c *memoryConn) Close() error                      { return nil }
func (c *memoryConn) LocalAddr() net.Addr               { return testAddr("local") }
func (c *memoryConn) RemoteAddr() net.Addr              { return testAddr("remote") }
func (c *memoryConn) SetDeadline(time.Time) error       { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error   { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error  { return nil }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

func TestBuildCMCCAuthRequest80(t *testing.T) {
	user := &User{Username: testCMCCUsername, Password: testCMCCPassword}
	request := buildCMCCAuthRequest(user, CMCCAuthMethod80, []byte{0x42})

	require.Len(t, request, 54)
	require.Equal(t, byte(1), request[0])
	require.Equal(t, byte(19), request[1])
	require.Equal(t, testCMCCUsername, string(request[2:21]))
	require.Equal(t, byte(32), request[21])
	require.Equal(t,
		"1adf689279da35824f8437556bc8295d38fee332f6b42458f5e6bacdfb7aa7ca",
		hex.EncodeToString(request[22:]),
	)
}

func TestBuildCMCCAuthRequest82(t *testing.T) {
	user := &User{Username: testCMCCUsername, Password: testCMCCPassword}
	request := buildCMCCAuthRequest(user, CMCCAuthMethod82, []byte{1, 2, 3, 4})

	require.Len(t, request, 75)
	require.Equal(t, byte(1), request[0])
	require.Equal(t, byte(19), request[1])
	require.Equal(t, testCMCCUsername, string(request[2:21]))
	require.Equal(t, byte(32), request[21])
	require.Equal(t,
		"d219186f50f158a5310eef407ceb76b8ffe9e98828eff1938f440efb981662d9",
		hex.EncodeToString(request[22:54]),
	)
	require.Equal(t, cmccAuthMethod82FixedData[:], request[54:])
}

func TestClientHandshakeCMCC80WireFormat(t *testing.T) {
	response := []byte{
		5, 0x42,
		1, 0,
		5, 0, 0, AtypIPv4, 127, 0, 0, 1, 0x1f, 0x90,
	}
	rawConn := newMemoryConn(response)
	conn := NewCMCCConn(rawConn)
	target := ParseAddr("example.com:443")
	user := &User{Username: testCMCCUsername, Password: testCMCCPassword}

	bind, err := ClientHandshakeCMCC(conn, target, CmdConnect, user, CMCCAuthMethod80)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:8080", bind.String())

	wire := rawConn.writes.Bytes()
	logical := make([]byte, len(wire))
	xorFF(logical, wire)
	require.Equal(t, []byte{5, 1, CMCCAuthMethod80}, logical[:3])
	require.Equal(t, buildCMCCAuthRequest(user, CMCCAuthMethod80, []byte{0x42}), logical[3:57])
	require.Equal(t, append([]byte{5, CmdConnect, 0}, target...), logical[57:])
}

func TestClientHandshakeCMCC82WireFormat(t *testing.T) {
	response := []byte{
		5, CMCCAuthMethod82, 1, 2, 3, 4,
		1, 0,
		5, 0, 0, AtypIPv4, 127, 0, 0, 1, 0, 0,
	}
	rawConn := newMemoryConn(response)
	conn := NewCMCCConn(rawConn)
	target := ParseAddr("example.com:80")
	user := &User{Username: testCMCCUsername, Password: testCMCCPassword}

	_, err := ClientHandshakeCMCC(conn, target, CmdConnect, user, CMCCAuthMethod82)
	require.NoError(t, err)

	wire := rawConn.writes.Bytes()
	logical := make([]byte, len(wire))
	xorFF(logical, wire)
	require.Equal(t, []byte{5, 1, CMCCAuthMethod82}, logical[:3])
	require.Equal(t, buildCMCCAuthRequest(user, CMCCAuthMethod82, []byte{1, 2, 3, 4}), logical[3:78])
	require.Equal(t, append([]byte{5, CmdConnect, 0}, target...), logical[78:])
}

func TestClientHandshakeCMCCUDPAssociate(t *testing.T) {
	response := []byte{
		5, 0x42,
		1, 0,
		5, 0, 0, AtypIPv4, 192, 0, 2, 79, 0x2a, 0x31,
	}
	rawConn := newMemoryConn(response)
	conn := NewCMCCConn(rawConn)
	associateAddr := ParseAddrToSocksAddr(&net.UDPAddr{IP: net.IPv4zero, Port: 0})
	user := &User{Username: testCMCCUsername, Password: testCMCCPassword}

	bind, err := ClientHandshakeCMCC(conn, associateAddr, CmdUDPAssociate, user, CMCCAuthMethod80)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.79:10801", bind.String())

	wire := rawConn.writes.Bytes()
	logical := make([]byte, len(wire))
	xorFF(logical, wire)
	requestOffset := 3 + 54
	require.Equal(t, append([]byte{5, CmdUDPAssociate, 0}, associateAddr...), logical[requestOffset:])
}

func TestCMCCConnOnlyObfuscatesWrites(t *testing.T) {
	rawConn := newMemoryConn([]byte("plain response"))
	conn := NewCMCCConn(rawConn)
	payload := []byte{0x00, 0x01, 0x7f, 0x80, 0xff}
	original := bytes.Clone(payload)

	n, err := conn.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Equal(t, original, payload, "Write must not mutate the caller's buffer")
	require.Equal(t, []byte{0xff, 0xfe, 0x80, 0x7f, 0x00}, rawConn.writes.Bytes())

	readBack, err := io.ReadAll(conn)
	require.NoError(t, err)
	require.Equal(t, []byte("plain response"), readBack)
	replaceable, ok := conn.(interface {
		ReaderReplaceable() bool
		WriterReplaceable() bool
	})
	require.True(t, ok)
	require.True(t, replaceable.ReaderReplaceable())
	require.False(t, replaceable.WriterReplaceable())
}

func TestCMCCConnWriteBufferObfuscatesAndTransfersOwnership(t *testing.T) {
	rawConn := newMemoryConn(nil)
	conn := NewCMCCConn(rawConn)
	payload := []byte{0x00, 0x01, 0x7f, 0x80, 0xff}
	buffer := buf.As(bytes.Clone(payload))

	require.NoError(t, conn.WriteBuffer(buffer))
	require.Equal(t, []byte{0xff, 0xfe, 0x80, 0x7f, 0x00}, rawConn.writes.Bytes())
}

type memoryPacketConn struct {
	writes []byte
	addr   net.Addr
}

func (c *memoryPacketConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, io.EOF }
func (c *memoryPacketConn) WriteTo(payload []byte, addr net.Addr) (int, error) {
	c.writes = bytes.Clone(payload)
	c.addr = addr
	return len(payload), nil
}
func (c *memoryPacketConn) Close() error                    { return nil }
func (c *memoryPacketConn) LocalAddr() net.Addr             { return testAddr("local") }
func (c *memoryPacketConn) SetDeadline(time.Time) error     { return nil }
func (c *memoryPacketConn) SetReadDeadline(time.Time) error { return nil }
func (c *memoryPacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

func TestCMCCPacketConnObfuscatesCompleteDatagram(t *testing.T) {
	rawConn := &memoryPacketConn{}
	conn := NewCMCCPacketConn(rawConn)
	relay := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 79), Port: 10801}
	payload := []byte{0, 0, 0, AtypIPv4, 8, 8, 8, 8, 0, 53, 0x12, 0x34}
	original := bytes.Clone(payload)

	n, err := conn.WriteTo(payload, relay)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.Equal(t, original, payload, "WriteTo must not mutate the caller's datagram")
	require.Equal(t, relay, rawConn.addr)
	expected := make([]byte, len(payload))
	xorFF(expected, payload)
	require.Equal(t, expected, rawConn.writes)
}

type chunkWriter struct {
	max int
	buf bytes.Buffer
}

func (w *chunkWriter) Write(payload []byte) (int, error) {
	if len(payload) > w.max {
		payload = payload[:w.max]
	}
	return w.buf.Write(payload)
}

func TestWriteFullHandlesShortWrites(t *testing.T) {
	writer := &chunkWriter{max: 2}
	payload := []byte("0123456789")
	require.NoError(t, writeFull(writer, payload))
	require.Equal(t, payload, writer.buf.Bytes())
}

func TestClientHandshakeCMCCValidatesInputBeforeWriting(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		user   *User
		method byte
		cmd    Command
	}{
		{name: "missing user", method: CMCCAuthMethod80, cmd: CmdConnect},
		{name: "missing username", user: &User{Password: "secret"}, method: CMCCAuthMethod80, cmd: CmdConnect},
		{name: "missing password", user: &User{Username: "user"}, method: CMCCAuthMethod80, cmd: CmdConnect},
		{name: "unknown method", user: &User{Username: "user", Password: "secret"}, method: 0x81, cmd: CmdConnect},
		{name: "bind command", user: &User{Username: "user", Password: "secret"}, method: CMCCAuthMethod80, cmd: CmdBind},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rawConn := newMemoryConn(nil)
			_, err := ClientHandshakeCMCC(NewCMCCConn(rawConn), ParseAddr("example.com:80"), testCase.cmd, testCase.user, testCase.method)
			require.Error(t, err)
			require.Empty(t, rawConn.writes.Bytes())
		})
	}
}
