package uusni

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

type dialFunc func(context.Context, string, string) (net.Conn, error)

func (f dialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func receiveHeader(conn net.Conn, key byte) ([]byte, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	xor(header, key)
	field := make([]byte, int(header[6]))
	if _, err := io.ReadFull(conn, field); err != nil {
		return nil, err
	}
	xor(field, key)
	return append(header, field...), nil
}

func waitError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}

func TestNoACKAndDialContextLifetime(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	var calls atomic.Int32
	client, err := NewClient(fixture(), dialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		if network != "tcp" || address != "203.0.113.10:5556" {
			t.Errorf("dialed %s %s instead of the configured node", network, address)
		}
		return left, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverDone := make(chan error, 1)
	go func() {
		header, err := receiveHeader(right, 77)
		if err == nil && !bytes.Equal(header[:4], []byte{'U', 'U', 1, 1}) {
			err = errors.New("custom header missing")
		}
		if err == nil {
			banner := []byte("server-first-banner")
			xor(banner, 77)
			_, err = right.Write(banner)
		}
		serverDone <- err
	}()
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := client.DialContext(ctx, "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	cancel() // DialContext owns establishment, not the returned stream lifetime.
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, len("server-first-banner"))
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "server-first-banner" {
		t.Fatal("an ACK read consumed payload, or handshake cancellation escaped", string(got), err)
	}
	if err := waitError(t, serverDone); err != nil || calls.Load() != 1 {
		t.Fatal("unexpected additional dial or mock error", err, calls.Load())
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(got); err == nil {
		t.Fatal("client shutdown left an active connection open")
	}
	if _, err := client.DialContext(context.Background(), "example.com", 443); !errors.Is(err, net.ErrClosed) {
		t.Fatal("closed client dialed again", err)
	}
}

func TestHandshakeCancellationAndShutdown(t *testing.T) {
	for _, action := range []string{"cancel", "deadline", "close"} {
		t.Run(action, func(t *testing.T) {
			left, right := net.Pipe()
			defer right.Close()
			entered := make(chan struct{})
			client, err := NewClient(fixture(), dialFunc(func(context.Context, string, string) (net.Conn, error) {
				close(entered)
				return left, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			if action == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 40*time.Millisecond)
			}
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.DialContext(ctx, "example.com", 443); done <- err }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("dial not entered")
			}
			// The peer never reads, so the custom header write cannot complete.
			switch action {
			case "cancel":
				cancel()
			case "close":
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err = waitError(t, done)
			if action == "deadline" {
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatal("deadline not propagated", err)
				}
			} else if err == nil {
				t.Fatal("blocked handshake survived cancellation/shutdown")
			}
			client.mu.Lock()
			pending, conns := len(client.pending), len(client.conns)
			client.mu.Unlock()
			if pending != 0 || conns != 0 {
				t.Fatal("failed handshake leaked resources", pending, conns)
			}
		})
	}
}

func TestCloseInterruptsPendingDial(t *testing.T) {
	entered := make(chan struct{})
	client, err := NewClient(fixture(), dialFunc(func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client.DialContext(context.Background(), "example.com", 443); done <- err }()
	<-entered
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("pending dial was not canceled", err)
	}
}

func TestRejectBeforeDialAndNeverFallback(t *testing.T) {
	var calls atomic.Int32
	sentinel := errors.New("synthetic node failure")
	client, err := NewClient(fixture(), dialFunc(func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, sentinel
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, host := range []string{"::1", "", "invalid/host"} {
		if _, err := client.DialContext(context.Background(), host, 443); err == nil {
			t.Fatal("unsupported target accepted", host)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid target caused network traffic")
	}
	if _, err := client.DialContext(context.Background(), "example.com", 443); !errors.Is(err, sentinel) || calls.Load() != 1 {
		t.Fatal("node error hidden or alternate transport tried", err, calls.Load())
	}
}

func TestTCPHalfClose(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = receiveHeader(raw, 77)
		if err == nil {
			var upload []byte
			upload, err = io.ReadAll(raw)
			xor(upload, 77)
			if err == nil && string(upload) != "request-until-fin" {
				err = errors.New("incorrect upload")
			}
		}
		if err == nil {
			reply := []byte("reply-after-fin")
			xor(reply, 77)
			_, err = raw.Write(reply)
		}
		serverDone <- err
	}()
	config := fixture()
	config.Server = "127.0.0.1"
	config.Port = listener.Addr().(*net.TCPAddr).Port
	client, err := NewClient(config, &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	raw, err := client.DialContext(context.Background(), "example.com", 80)
	if err != nil {
		t.Fatal(err)
	}
	conn := raw.(*Conn)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("request-until-fin")); err != nil {
		t.Fatal(err)
	}
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(conn)
	if err != nil || string(reply) != "reply-after-fin" {
		t.Fatal("write FIN closed the download direction", string(reply), err)
	}
	if err := waitError(t, serverDone); err != nil {
		t.Fatal(err)
	}
}
