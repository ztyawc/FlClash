package qyproxy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func testOptions() Options {
	return Options{Server: "127.0.0.1", Port: 1, Role: Primary, Username: "test-user", Password: "test-secret", SN: "test-device", GameID: 456, ServerID: 123, GameArea: "hk"}
}
func TestWireFixtures(t *testing.T) {
	var f struct {
		Configure map[string]string
		CHAP      string
		TCP       string `json:"tcp_init"`
		UDP       string `json:"udp_init"`
	}
	b, err := os.ReadFile("testdata/synthetic_wire.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &f))
	o := testOptions()
	require.NoError(t, o.normalize())
	for _, role := range []string{Primary, CN2, CN2Download} {
		o.Role = role
		require.Equal(t, f.Configure[role], hex.EncodeToString(configure(o)))
	}
	response, err := challengeResponse(o, message{4, 1, 17, []byte{3, 'a', 'b', 'c'}})
	require.NoError(t, err)
	require.Equal(t, f.CHAP, hex.EncodeToString(response))
	var token [16]byte
	var mask [8]byte
	for i := range token {
		token[i] = byte(i)
	}
	for i := range mask {
		mask[i] = byte(i)
	}
	source := netip.MustParseAddrPort("192.0.2.2:32100")
	o.Role = Primary
	require.Equal(t, f.TCP, hex.EncodeToString(sessionInit(o, token, mask, source, netip.MustParseAddrPort("192.0.2.1:443"))))
	o.Role = CN2
	require.Equal(t, f.UDP, hex.EncodeToString(sessionInit(o, token, mask, source, netip.AddrPort{})))
}

type smallReader struct{ io.Reader }

func (r smallReader) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.Reader.Read(b)
}
func TestControlBoundaries(t *testing.T) {
	b, _ := hex.DecodeString("04011100080361626301082a0004")
	r := smallReader{bytes.NewReader(b)}
	m, e := readMessage(r)
	require.NoError(t, e)
	require.Equal(t, message{4, 1, 17, []byte{3, 'a', 'b', 'c'}}, m)
	m, e = readMessage(r)
	require.NoError(t, e)
	require.Equal(t, byte(8), m.code)
	for _, wire := range []string{"0102000003", "010200ffff", "010200000658"} {
		b, _ = hex.DecodeString(wire)
		_, e = readMessage(bytes.NewReader(b))
		require.Error(t, e)
	}
	for _, b := range [][]byte{{7}, {7, 0}, {7, 1}, {7, 5, 1}} {
		_, e = parseOptions(b, true)
		require.Error(t, e)
	}
	token := bytes.Repeat([]byte{9}, 16)
	for _, code := range []byte{3, 5, 7} {
		body := token
		if code == 5 {
			body = append([]byte{0, 0, 16}, token...)
		}
		if code == 7 {
			body = append([]byte{0, 0, 18, 2, 16}, token...)
		}
		got, e := parseSuccess(message{code: code, body: body})
		require.NoError(t, e)
		require.Equal(t, token, got[:])
	}
	_, e = parseSuccess(message{code: 7, body: []byte{0, 0, 3, 2, 16, 0}})
	require.Error(t, e)
	require.Error(t, checkACK(append([]byte{5, 3}, make([]byte, 12)...)))
}
func TestUDPFixtures(t *testing.T) {
	mask := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	target := netip.MustParseAddrPort("192.0.2.1:53")
	for _, text := range []string{"ABCDEFGHI", "AB"} {
		b := udpFrame(target, []byte(text), mask)
		require.Equal(t, []byte{0x40, 0x40}, b[10:12])
		ap, p, e := parseUDP(b, mask)
		require.NoError(t, e)
		require.Equal(t, target, ap)
		require.Equal(t, text, string(p))
	}
	b := udpFrame(target, []byte("x"), mask)
	b[0] = 1
	_, _, e := parseUDP(b, mask)
	require.Error(t, e)
}

type shortWriterConn struct {
	net.Conn
	data bytes.Buffer
}

func (c *shortWriterConn) Write(b []byte) (int, error) {
	if len(b) > 3 {
		b = b[:3]
	}
	return c.data.Write(b)
}
func TestTCPShortWriteOffset(t *testing.T) {
	raw := &shortWriterConn{}
	c := &streamConn{Conn: raw, mask: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	a, b := []byte("ABCDE"), []byte("FGHIJKLMNOPQ")
	n, e := c.Write(a)
	require.NoError(t, e)
	require.Equal(t, 5, n)
	n, e = c.Write(b)
	require.NoError(t, e)
	require.Equal(t, 12, n)
	require.Equal(t, "ABCDE", string(a))
	require.Equal(t, "FGHIJKLMNOPQ", string(b))
	require.Equal(t, "4040404040404040484848484848485850", hex.EncodeToString(raw.data.Bytes()))
	require.False(t, c.ReaderReplaceable())
	require.False(t, c.WriterReplaceable())
}
