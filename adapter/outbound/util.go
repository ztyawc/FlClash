package outbound

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/socks5"
)

// Preserve raw numeric proxy options: weak decoding may otherwise truncate a
// fractional port or key before constructor validation sees it.
func strictUnsignedInteger(value any, max uint64) (int, error) {
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

func serializesSocksAddr(metadata *C.Metadata) []byte {
	var buf [][]byte
	addrType := metadata.AddrType()
	p := uint(metadata.DstPort)
	port := []byte{uint8(p >> 8), uint8(p & 0xff)}
	switch addrType {
	case C.AtypDomainName:
		lenM := uint8(len(metadata.Host))
		host := []byte(metadata.Host)
		buf = [][]byte{{socks5.AtypDomainName, lenM}, host, port}
	case C.AtypIPv4:
		host := metadata.DstIP.AsSlice()
		buf = [][]byte{{socks5.AtypIPv4}, host, port}
	case C.AtypIPv6:
		host := metadata.DstIP.AsSlice()
		buf = [][]byte{{socks5.AtypIPv6}, host, port}
	}
	return bytes.Join(buf, nil)
}

func resolveIPWithResolver(ctx context.Context, host string, prefer C.DNSPrefer, r resolver.Resolver) (netip.Addr, error) {
	switch prefer {
	case C.IPv4Only:
		return resolver.ResolveIPv4WithResolver(ctx, host, r)
	case C.IPv6Only:
		return resolver.ResolveIPv6WithResolver(ctx, host, r)
	case C.IPv6Prefer:
		return resolver.ResolveIPPrefer6WithResolver(ctx, host, r)
	default:
		return resolver.ResolveIPWithResolver(ctx, host, r)
	}
}

func resolveUDPAddr(ctx context.Context, network, address string, prefer C.DNSPrefer) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ip, err := resolveIPWithResolver(resolver.WithInitiator(ctx, resolver.InitiatorProxy), host, prefer, resolver.ProxyServerHostResolver)

	if err != nil {
		return nil, err
	}

	ip, port = resolver.LookupIP4P(ip, port)

	var uint16Port uint16
	if port, err := strconv.ParseUint(port, 10, 16); err == nil {
		uint16Port = uint16(port)
	} else {
		return nil, err
	}
	// our resolver always unmap before return, so unneeded unmap at here
	// which is different with net.ResolveUDPAddr maybe return 4in6 address
	// 4in6 addresses can cause some strange effects on sing-based code
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, uint16Port)), nil
}

func safeConnClose(c net.Conn, err error) {
	if err != nil && c != nil {
		_ = c.Close()
	}
}
