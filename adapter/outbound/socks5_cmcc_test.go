package outbound

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/structure"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/socks5"

	SBufio "github.com/metacubex/sing/common/bufio"
	"github.com/stretchr/testify/require"
)

func TestParseCMCCAuthMethod(t *testing.T) {
	for _, testCase := range []struct {
		value  string
		method byte
	}{
		{value: "", method: 0},
		{value: "0", method: 0},
		{value: "0x80", method: socks5.CMCCAuthMethod80},
		{value: "0X80", method: socks5.CMCCAuthMethod80},
		{value: "80", method: socks5.CMCCAuthMethod80},
		{value: "128", method: socks5.CMCCAuthMethod80},
		{value: " 0x82 ", method: socks5.CMCCAuthMethod82},
		{value: "82", method: socks5.CMCCAuthMethod82},
		{value: "130", method: socks5.CMCCAuthMethod82},
	} {
		method, err := parseCMCCAuthMethod(testCase.value)
		require.NoError(t, err)
		require.Equal(t, testCase.method, method)
	}

	_, err := parseCMCCAuthMethod("0x81")
	require.ErrorContains(t, err, "unsupported cmcc-auth-method")
}

func TestCMCCAuthMethodWeaklyDecodesYAMLInteger(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{
		TagName:          "proxy",
		WeaklyTypedInput: true,
		KeyReplacer:      structure.DefaultKeyReplacer,
	})
	option := &Socks5Option{}
	require.NoError(t, decoder.Decode(map[string]any{
		"name":             "cmcc",
		"server":           "127.0.0.1",
		"port":             10800,
		"cmcc-auth-method": 0x80,
	}, option))
	require.Equal(t, "128", option.CMCCAuthMethod)
	method, err := parseCMCCAuthMethod(option.CMCCAuthMethod)
	require.NoError(t, err)
	require.Equal(t, socks5.CMCCAuthMethod80, method)
}

func TestNewSocks5ValidatesCMCCCredentials(t *testing.T) {
	base := Socks5Option{
		Name:           "cmcc",
		Server:         "127.0.0.1",
		Port:           10800,
		CMCCAuthMethod: "0x80",
	}

	_, err := NewSocks5(base)
	require.ErrorContains(t, err, "requires a username")

	base.UserName = "1234567899876543210"
	_, err = NewSocks5(base)
	require.ErrorContains(t, err, "requires a password")

	base.Password = "secret"
	proxy, err := NewSocks5(base)
	require.NoError(t, err)
	require.Equal(t, socks5.CMCCAuthMethod80, proxy.cmccAuthMethod)
}

func TestCMCCWriterIsNotBypassedByBufferedRelay(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = server.Close() })

	proxy := &Socks5{
		Base:   NewBase(BaseOption{Name: "cmcc", Addr: "127.0.0.1:10800", Type: C.Socks5}),
		option: &Socks5Option{},
	}
	conn := NewConn(socks5.NewCMCCConn(client), proxy)
	payload := []byte{0x00, 0x01, 0x7f, 0x80, 0xff}
	received := make(chan []byte, 1)
	go func() {
		wire := make([]byte, len(payload))
		_, _ = io.ReadFull(server, wire)
		received <- wire
	}()

	_, err := SBufio.Copy(conn, bytes.NewReader(payload))
	require.NoError(t, err)
	select {
	case wire := <-received:
		require.Equal(t, []byte{0xff, 0xfe, 0x80, 0x7f, 0x00}, wire)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for buffered relay write")
	}
}
