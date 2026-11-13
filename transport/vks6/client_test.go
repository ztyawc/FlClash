package vks6

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type dialFunc func(context.Context, string, string) (net.Conn, error)

func (f dialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestClientEndToEndAndClose(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	config := testConfig()
	config.PortEnd = config.Port
	client, err := NewClient(config, dialFunc(func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "203.0.113.10:8050" {
			return nil, errors.New("unexpected node address")
		}
		return left, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverDone := make(chan error, 1)
	go func() {
		want, _ := Header(config, netip.MustParseAddrPort("43.174.102.54:80"))
		request := make([]byte, len(want))
		if _, err := io.ReadFull(right, request); err != nil {
			serverDone <- err
			return
		}
		if !bytes.Equal(request, want) {
			serverDone <- errors.New("wrong wire request")
			return
		}
		if _, err := right.Write([]byte{6, 0}); err != nil {
			serverDone <- err
			return
		}
		request = make([]byte, 4)
		if _, err := io.ReadFull(right, request); err != nil {
			serverDone <- err
			return
		}
		xor(request)
		if string(request) != "ping" {
			serverDone <- errors.New("wrong uploaded payload")
			return
		}
		response := []byte("pong")
		xor(response)
		_, err := right.Write(response)
		serverDone <- err
	}()
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := client.DialContext(ctx, netip.MustParseAddrPort("43.174.102.54:80"))
	if err != nil {
		t.Fatal(err)
	}
	cancel() // Establishment context must not close the established stream.
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "pong" {
		t.Fatalf("download payload %q: %v", got, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(got); err == nil {
		t.Fatal("client close left established stream open")
	}
	if _, err := client.DialContext(context.Background(), netip.MustParseAddrPort("43.174.102.54:80")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed client dial: %v", err)
	}
}

func TestClientRejectsNodeReplyAndTimeout(t *testing.T) {
	for _, mode := range []string{"rejected", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			left, right := net.Pipe()
			defer right.Close()
			client, err := NewClient(testConfig(), dialFunc(func(context.Context, string, string) (net.Conn, error) {
				return left, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			go func() {
				request := make([]byte, 34)
				_, _ = io.ReadFull(right, request)
				if mode == "rejected" {
					_, _ = right.Write([]byte{6, 1})
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err = client.DialContext(ctx, netip.MustParseAddrPort("43.174.102.54:80"))
			if err == nil {
				t.Fatal("node failure accepted")
			}
			if mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout error: %v", err)
			}
		})
	}
}

type shortConn struct {
	net.Conn
	bytes.Buffer
	limit int
	zero  bool
}

func (c *shortConn) Write(p []byte) (int, error) {
	if c.zero {
		return 0, nil
	}
	n := len(p)
	if n > c.limit {
		n = c.limit
	}
	return c.Buffer.Write(p[:n])
}
func (c *shortConn) Read(p []byte) (int, error) { return c.Buffer.Read(p) }

func TestConnShortWritesDoNotBypassXOR(t *testing.T) {
	input := []byte(strings.Repeat("0123456789", 10000))
	original := bytes.Clone(input)
	raw := &shortConn{limit: 7}
	conn := &Conn{Conn: raw}
	if conn.ReaderReplaceable() || conn.WriterReplaceable() {
		t.Fatal("copy path can bypass XOR")
	}
	n, err := conn.Write(input)
	if err != nil || n != len(input) || !bytes.Equal(input, original) {
		t.Fatalf("write count/mutation: %d, %v", n, err)
	}
	for i, b := range raw.Bytes() {
		if b != input[i]^dataKey {
			t.Fatalf("wire byte %d is not XORed", i)
		}
	}
	got, err := io.ReadAll(conn)
	if err != nil || !bytes.Equal(got, input) {
		t.Fatalf("read XOR: %v", err)
	}
	raw.zero = true
	if _, err := conn.Write([]byte("x")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("zero writer: %v", err)
	}
}
