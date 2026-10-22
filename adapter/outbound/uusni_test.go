package outbound

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	N "github.com/metacubex/mihomo/common/net"
	C "github.com/metacubex/mihomo/constant"
	SBufio "github.com/metacubex/sing/common/bufio"
	"github.com/stretchr/testify/require"
)

type uusniTestDialer struct {
	dial func(context.Context, string, string) (net.Conn, error)
}

func (d uusniTestDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dial(ctx, network, addr)
}
func (d uusniTestDialer) ListenPacket(context.Context, string, string, netip.AddrPort) (net.PacketConn, error) {
	return nil, C.ErrNotSupport
}

func testUUSNIOption() UUSNIOption {
	return UUSNIOption{Name: "synthetic UU", Server: "203.0.113.10", Port: 5556, ObfsKey: 77, SNICrypt: true}
}

// Independently decode the mock server's header; never call the client encoder.
func readUUSNITarget(raw net.Conn, node string, key byte, encrypted bool) (string, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(raw, header); err != nil {
		return "", err
	}
	for i := range header {
		header[i] ^= key
	}
	wantFlag := byte(0)
	if encrypted {
		wantFlag = 1
	}
	if !bytes.Equal(header[:3], []byte{'U', 'U', 1}) || header[3] != wantFlag {
		return "", errors.New("incorrect custom header or crypt flag")
	}
	field := make([]byte, int(header[6]))
	if _, err := io.ReadFull(raw, field); err != nil {
		return "", err
	}
	for i := range field {
		field[i] ^= key
	}
	if encrypted {
		if len(field) < 33 || field[0] != 16 || !bytes.Equal(field[1:17], make([]byte, 16)) {
			return "", errors.New("incorrect GCM IV format")
		}
		keyBytes := bytes.Repeat([]byte{'U'}, 16)
		copy(keyBytes, node)
		block, err := aes.NewCipher(keyBytes)
		if err != nil {
			return "", err
		}
		aead, err := cipher.NewGCMWithNonceSize(block, 16)
		if err != nil {
			return "", err
		}
		field, err = aead.Open(nil, field[1:17], field[17:], nil)
		if err != nil {
			return "", err
		}
	}
	return net.JoinHostPort(string(field), strconv.Itoa(int(binary.BigEndian.Uint16(header[4:6])))), nil
}

func TestUUSNIMetadataAndCoreDialer(t *testing.T) {
	for i, metadata := range []*C.Metadata{
		{Host: "unresolved.invalid", DstIP: netip.MustParseAddr("198.18.0.1"), DstPort: 80},
		{Host: "unresolved.invalid", DstIP: netip.MustParseAddr("192.0.2.3"), DstPort: 80, DNSMode: C.DNSHosts},
		{Host: "unresolved.invalid", DstIP: netip.MustParseAddr("192.0.2.3"), DstPort: 80, DNSMode: C.DNSMapping},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			left, right := net.Pipe()
			defer right.Close()
			observed := make(chan string, 1)
			errCh := make(chan error, 1)
			go func() {
				target, err := readUUSNITarget(right, "203.0.113.10:5556", 77, true)
				observed <- target
				errCh <- err
				_, _ = io.Copy(io.Discard, right)
			}()
			option := testUUSNIOption()
			option.DialerForAPI = uusniTestDialer{dial: func(_ context.Context, network, addr string) (net.Conn, error) {
				require.Equal(t, "tcp", network)
				require.Equal(t, "203.0.113.10:5556", addr)
				return left, nil
			}}
			option.ProviderName, option.DialerProxy = "test provider", "test chain"
			p, err := NewUUSNI(option)
			require.NoError(t, err)
			defer p.Close()
			conn, err := p.DialContext(context.Background(), metadata)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, <-errCh)
			want := "192.0.2.3:80"
			if i == 0 {
				want = "unresolved.invalid:80" // Never resolve or send the Fake-IP.
			}
			require.Equal(t, want, <-observed)
			require.Equal(t, "unresolved.invalid", metadata.Host, "do not modify caller metadata")
			require.Equal(t, C.Chain{"synthetic UU"}, conn.Chains())
			require.Equal(t, C.Chain{"test provider"}, conn.ProviderChains())
			require.Equal(t, "test chain", p.ProxyInfo().DialerProxy)
		})
	}
}

func TestUUSNIRejectsTargetsAndUDPBeforeDial(t *testing.T) {
	var calls atomic.Int32
	option := testUUSNIOption()
	option.DialerForAPI = uusniTestDialer{dial: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("must not dial")
	}}
	p, err := NewUUSNI(option)
	require.NoError(t, err)
	defer p.Close()
	for _, m := range []*C.Metadata{nil, {NetWork: C.UDP, Host: "example.com", DstPort: 53},
		{DstIP: netip.MustParseAddr("2001:db8::1"), DstPort: 443}, {Host: "example.com"}, {Host: "bad/host", DstPort: 443}} {
		_, err := p.DialContext(context.Background(), m)
		require.Error(t, err)
	}
	_, err = p.ListenPacketContext(context.Background(), &C.Metadata{NetWork: C.UDP, Host: "example.com", DstPort: 53})
	require.ErrorIs(t, err, C.ErrNotSupport)
	require.Zero(t, calls.Load())
}

func TestUUSNIBufferedRelayCannotBypassXOR(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	_ = right.SetDeadline(time.Now().Add(3 * time.Second))
	option := testUUSNIOption()
	option.SNICrypt = false
	option.DialerForAPI = uusniTestDialer{dial: func(context.Context, string, string) (net.Conn, error) { return left, nil }}
	p, err := NewUUSNI(option)
	require.NoError(t, err)
	defer p.Close()
	payload := bytes.Repeat([]byte{0, 1, 127, 128, 255}, 30000)
	original := bytes.Clone(payload)
	serverDone := make(chan error, 1)
	go func() {
		defer right.Close()
		target, err := readUUSNITarget(right, "203.0.113.10:5556", 77, false)
		if err == nil && target != "example.com:80" {
			err = fmt.Errorf("wrong target %s", target)
		}
		wire := make([]byte, len(payload))
		if err == nil {
			_, err = io.ReadFull(right, wire)
		}
		if err == nil {
			for i, b := range wire {
				if b != original[i]^77 {
					err = errors.New("core buffered copy bypassed the XOR writer")
					break
				}
			}
		}
		if err == nil {
			_, err = right.Write(wire)
		}
		serverDone <- err
	}()
	conn, err := p.DialContext(context.Background(), &C.Metadata{Host: "example.com", DstPort: 80})
	require.NoError(t, err)
	n, err := SBufio.Copy(conn, bytes.NewReader(payload))
	require.NoError(t, err)
	require.EqualValues(t, len(payload), n)
	var received bytes.Buffer
	_, err = SBufio.Copy(&received, conn)
	require.NoError(t, err)
	require.Equal(t, original, received.Bytes())
	require.Equal(t, original, payload, "caller buffer modified")
	require.NoError(t, <-serverDone)
}

type uusniMockConn struct {
	net.Conn
	key byte
}

func (c *uusniMockConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	for i := 0; i < n; i++ {
		p[i] ^= c.key
	}
	return n, err
}
func (c *uusniMockConn) Write(p []byte) (int, error) {
	b := bytes.Clone(p)
	for i := range b {
		b[i] ^= c.key
	}
	return c.Conn.Write(b)
}

func TestUUSNIVerifiedHTTPSRoundTrip(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(strconv.FormatBool(encrypted), func(t *testing.T) {
			payload := bytes.Repeat([]byte("large upload and download\x00\xff"), 40000)
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(body, payload) {
					http.Error(w, "wrong request body", http.StatusBadRequest)
					return
				}
				_, _ = w.Write(body)
			}))
			defer origin.Close()
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			node := listener.Addr().String()
			serverDone := make(chan error, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer raw.Close()
				_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
				target, err := readUUSNITarget(raw, node, 77, encrypted)
				if err == nil && target != origin.Listener.Addr().String() {
					err = fmt.Errorf("target changed: %s", target)
				}
				if err == nil {
					var upstream net.Conn
					upstream, err = net.DialTimeout("tcp", target, time.Second)
					if err == nil {
						defer upstream.Close()
						N.Relay(&uusniMockConn{Conn: raw, key: 77}, upstream)
					}
				}
				serverDone <- err
			}()
			option := testUUSNIOption()
			option.Server = "127.0.0.1"
			option.Port = listener.Addr().(*net.TCPAddr).Port
			option.SNICrypt = encrypted
			p, err := NewUUSNI(option)
			require.NoError(t, err)
			defer p.Close()
			transport := origin.Client().Transport.(*http.Transport).Clone()
			require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
			transport.DisableKeepAlives = true
			transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
				host, portText, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				port, _ := strconv.Atoi(portText)
				return p.DialContext(ctx, &C.Metadata{DstIP: netip.MustParseAddr(host), DstPort: uint16(port)})
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
			response, err := client.Post(origin.URL, "application/octet-stream", bytes.NewReader(payload))
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Equal(t, payload, body)
			select {
			case err := <-serverDone:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("mock relay did not finish")
			}
		})
	}
}
