package outbound

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/uusni"
)

type UUSNIOption struct {
	BasicOption
	Name      string `proxy:"name"`
	Server    string `proxy:"server"`
	Port      any    `proxy:"port"`
	ObfsKey   any    `proxy:"obfs-key"`
	SNICrypt  any    `proxy:"sni-crypt"`
	CustomSNI *bool  `proxy:"custom-sni,omitempty"`
	OuterTLS  bool   `proxy:"outer-tls,omitempty"`
	TLS       bool   `proxy:"tls,omitempty"`
	UDP       bool   `proxy:"udp,omitempty"`
	Network   string `proxy:"network,omitempty"`
}

type UUSNI struct {
	*Base
	client      *uusni.Client
	dialerProxy string
}

// Preserve raw numeric input until validation: weak decoding must not turn
// a fractional XOR byte into an integer or an omitted key into zero.
func uusniInteger(value any, max uint64) (int, error) {
	v := reflect.ValueOf(value)
	var text string
	if v.IsValid() {
		switch v.Kind() {
		case reflect.String:
			text = strings.TrimSpace(v.String())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			text = strconv.FormatInt(v.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			text = strconv.FormatUint(v.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			n := v.Float()
			if n >= 0 && n <= float64(max) && n == math.Trunc(n) {
				return int(n), nil
			}
		}
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil || n > max {
		return 0, errors.New("an explicit decimal integer in range is required")
	}
	return int(n), nil
}

func uusniCrypt(value any) (bool, error) {
	if v, ok := value.(bool); ok {
		return v, nil
	}
	if v, ok := value.(string); ok {
		if v == "true" || v == "false" {
			return v == "true", nil
		}
	}
	n, err := uusniInteger(value, 1)
	return n == 1, err
}

func NewUUSNI(o UUSNIOption) (*UUSNI, error) {
	port, err := uusniInteger(o.Port, 65535)
	if err != nil || port == 0 {
		return nil, errors.New("uusni: port must be an integer between 1 and 65535")
	}
	key, err := uusniInteger(o.ObfsKey, 255)
	if err != nil {
		return nil, fmt.Errorf("uusni: obfs-key: %w", err)
	}
	crypt, err := uusniCrypt(o.SNICrypt)
	if err != nil {
		return nil, errors.New("uusni: sni-crypt must explicitly be true/false or 1/0")
	}
	if o.UDP || o.TLS || o.OuterTLS || o.CustomSNI != nil && !*o.CustomSNI || o.Network != "" && o.Network != "tcp" {
		return nil, errors.New("uusni: only custom SNI/TCP without outer TLS is supported")
	}
	if o.IPVersion == C.IPv6Only {
		return nil, errors.New("uusni: the node requires IPv4")
	}
	p := &UUSNI{Base: NewBase(BaseOption{Name: o.Name, Addr: net.JoinHostPort(o.Server, strconv.Itoa(port)), Type: C.UUSNI, ProviderName: o.ProviderName, TFO: o.TFO, MPTCP: o.MPTCP, Interface: o.Interface, RoutingMark: o.RoutingMark, Prefer: C.IPv4Only}), dialerProxy: o.DialerProxy}
	p.dialer = o.NewDialer(p.DialOptions())
	p.client, err = uusni.NewClient(uusni.Config{Server: o.Server, Port: port, ObfsKey: key, SNICrypt: crypt}, p.dialer)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *UUSNI) DialContext(ctx context.Context, m *C.Metadata) (C.Conn, error) {
	if m == nil || m.NetWork != C.TCP {
		return nil, errors.New("uusni: only TCP targets are supported")
	}
	// Pure honors explicit hosts/mapping IPs; Fake-IP keeps its domain so the
	// remote node receives the original hostname rather than a synthetic IP.
	m = m.Pure()
	host := m.Host
	if host == "" && m.DstIP.IsValid() {
		host = m.DstIP.Unmap().String()
	}
	c, err := p.client.DialContext(ctx, host, m.DstPort)
	if err != nil {
		return nil, err
	}
	return NewConn(c, p), nil
}

func (p *UUSNI) ProxyInfo() C.ProxyInfo {
	info := p.Base.ProxyInfo()
	info.DialerProxy = p.dialerProxy
	return info
}

func (p *UUSNI) Close() error { return p.client.Close() }

var _ ProxyAdapter = (*UUSNI)(nil)
