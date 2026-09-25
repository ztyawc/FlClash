// Package qyproxy implements the observed IPv4 QyProxy game transport.
// It does not implement SOCKS authentication, QyVpn, or Hop.
package qyproxy

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Primary     = "primary"
	CN2         = "cn2"
	CN2Download = "cn2_download"
)

var (
	controlMagic = []byte{0xae, 0xdb, 0xcb, 0x43}
	dataMagic    = []byte{0xae, 0xdb, 0xcb, 0xec}
	ErrAuth      = errors.New("qyproxy: authentication rejected; refresh credentials and reload")
	ErrClosed    = errors.New("qyproxy: closed")
	ErrProtocol  = errors.New("qyproxy: malformed or unsupported protocol message")
	ErrIPv6      = errors.New("qyproxy: only IPv4 destinations are supported")
	ErrOversize  = errors.New("qyproxy: UDP packet requires unsupported fragmentation")
	ErrSession   = errors.New("qyproxy: data session rejected or released")
)

// Options contains node credentials, never APP/API tokens. Port is the actual
// control port, not the generic port in a native APP profile.
type Options struct {
	Server     string
	Port       uint16
	Role       string
	Username   string
	Password   string
	SN         string
	GameID     uint32
	ServerID   uint32
	ClientType uint32
	GameArea   string
	Zone       string
	Product    string
	Version    string
}

func (o *Options) normalize() error {
	if o.Role == "" {
		o.Role = Primary
	}
	if o.ClientType == 0 {
		o.ClientType = 20
	}
	if o.GameArea == "" {
		o.GameArea = "default"
	}
	if o.Zone == "" {
		o.Zone = o.GameArea
	}
	if o.Product == "" {
		o.Product = "hy-android"
	}
	if o.Version == "" {
		o.Version = "1.0.0"
	}
	if o.Role != Primary && o.Role != CN2 && o.Role != CN2Download {
		return errors.New("qyproxy: role must be primary, cn2 or cn2_download")
	}
	if o.Server == "" || o.Port == 0 || o.GameID == 0 || o.ServerID == 0 {
		return errors.New("qyproxy: server, port, game-id and server-id are required")
	}
	for _, s := range []string{o.Username, o.Password, o.SN, o.GameArea, o.Zone, o.Product, o.Version} {
		if len(s) == 0 || len(s) > 128 || strings.ContainsRune(s, 0) {
			return errors.New("qyproxy: invalid credential or protocol text length")
		}
	}
	if strings.Contains(o.Username, "&") || strings.Contains(o.SN, "&") || len(o.wireUsername()) > 254 {
		return errors.New("qyproxy: invalid composed username")
	}
	return nil
}

func (o Options) wireUsername() string {
	return o.Username + "&" + strconv.FormatUint(uint64(o.ClientType), 10) + "&" + strconv.FormatUint(uint64(o.GameID), 10) + "&" + o.SN
}
func (o Options) dataArea() string {
	if o.Role == Primary {
		return o.GameArea
	}
	return o.Role
}
func (o Options) mtu() int {
	if o.Role == Primary {
		return 1400
	}
	return 1200
}

type message struct {
	kind, code, id byte
	body           []byte
}

func frame(kind, code, id byte, body []byte) []byte {
	b := make([]byte, 5+len(body))
	b[0], b[1], b[2] = kind, code, id
	binary.BigEndian.PutUint16(b[3:], uint16(4+len(body)))
	copy(b[5:], body)
	return b
}
func readMessage(r io.Reader) (message, error) {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return message{}, err
	}
	n := int(binary.BigEndian.Uint16(h[3:]))
	if n < 4 || n > 2047 {
		return message{}, ErrProtocol
	}
	m := message{h[0], h[1], h[2], make([]byte, n-4)}
	_, err := io.ReadFull(r, m.body)
	return m, err
}
func writeFull(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func option(kind byte, value []byte, inclusive bool) []byte {
	n := len(value)
	if inclusive {
		n += 2
	}
	return append([]byte{kind, byte(n)}, value...)
}
func parseOptions(b []byte, inclusive bool) (map[byte][]byte, error) {
	out := make(map[byte][]byte)
	for len(b) > 0 {
		if len(b) < 2 {
			return nil, ErrProtocol
		}
		n := int(b[1])
		if !inclusive {
			n += 2
		}
		if n < 2 || n > len(b) {
			return nil, ErrProtocol
		}
		if _, ok := out[b[0]]; ok {
			return nil, ErrProtocol
		}
		out[b[0]] = b[2:n]
		b = b[n:]
	}
	return out, nil
}
func be16(n uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, n); return b }
func configure(o Options) []byte {
	zone, area := o.Zone, o.GameArea
	if o.Role != Primary {
		zone, area = o.Role, o.Role
	}
	b := option(7, be16(1200), true)
	b = append(b, option(2, []byte{1, 2, 0x21}, true)...)
	id := make([]byte, 4)
	binary.BigEndian.PutUint32(id, o.ServerID)
	b = append(b, option(15, id, true)...)
	for _, v := range []struct {
		k byte
		s string
	}{{8, area}, {11, o.Product}, {12, o.Version}, {17, "vpn"}, {13, o.GameArea}, {14, zone}} {
		b = append(b, option(v.k, append([]byte{byte(len(v.s))}, v.s...), true)...)
	}
	b = append(b, option(3, []byte{4, 5}, true)...)
	return append(append([]byte{}, controlMagic...), frame(1, 1, 0, b)...)
}
func challengeResponse(o Options, m message) ([]byte, error) {
	if len(m.body) == 0 || m.body[0] == 0 || int(m.body[0])+1 > len(m.body) {
		return nil, ErrProtocol
	}
	h := md5.New()
	h.Write([]byte{m.id})
	h.Write([]byte(o.Password))
	h.Write(m.body[1 : 1+int(m.body[0])])
	b := append([]byte{16}, h.Sum(nil)...)
	b = append(b, o.wireUsername()...)
	return frame(4, 2, m.id, b), nil
}

type negotiated struct {
	tcp, udp []uint16
	dns      netip.AddrPort
	interval time.Duration
	retries  uint16
}

func parseConfigure(b []byte) (negotiated, error) {
	n := negotiated{interval: 15 * time.Second, retries: 3}
	opts, err := parseOptions(b, true)
	if err != nil {
		return n, err
	}
	a := opts[3]
	if len(a) != 2 || a[0] != 4 || a[1] != 5 {
		return n, ErrProtocol
	}
	if v, ok := opts[4]; ok {
		if len(v) != 4 {
			return n, ErrProtocol
		}
		n.tcp = []uint16{binary.BigEndian.Uint16(v)}
		n.udp = []uint16{binary.BigEndian.Uint16(v[2:])}
	}
	for _, item := range []struct {
		k   byte
		out *[]uint16
	}{{19, &n.tcp}, {20, &n.udp}} {
		if v, ok := opts[item.k]; ok {
			if len(v) < 1 || len(v) != 1+2*int(v[0]) {
				return n, ErrProtocol
			}
			ports := []uint16{}
			for i := 0; i < int(v[0]) && i < 4; i++ {
				p := binary.BigEndian.Uint16(v[1+i*2:])
				found := false
				for _, x := range ports {
					found = found || x == p
				}
				if !found {
					ports = append(ports, p)
				}
			}
			sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
			*item.out = ports
		}
	}
	for _, ports := range [][]uint16{n.tcp, n.udp} {
		if len(ports) == 0 {
			return n, ErrProtocol
		}
		for _, p := range ports {
			if p == 0 {
				return n, ErrProtocol
			}
		}
	}
	if v, ok := opts[6]; ok {
		if len(v) != 4 {
			return n, ErrProtocol
		}
		s := binary.BigEndian.Uint16(v)
		n.retries = binary.BigEndian.Uint16(v[2:])
		if s < 5 || s > 3600 || n.retries == 0 {
			return n, ErrProtocol
		}
		n.interval = time.Duration(s) * time.Second
	}
	if v, ok := opts[9]; ok {
		if len(v) < 1 || int(v[0]) != len(v)-1 {
			return n, ErrProtocol
		}
		ip, e := netip.ParseAddr(string(v[1:]))
		if e != nil || !ip.Is4() {
			return n, ErrProtocol
		}
		// CN2 may announce an address without a DNS port; its DNS queries
		// travel as ordinary UDP and do not require this optional endpoint.
		if p, ok := opts[18]; ok {
			if len(p) != 2 || binary.BigEndian.Uint16(p) == 0 {
				return n, ErrProtocol
			}
			n.dns = netip.AddrPortFrom(ip, binary.BigEndian.Uint16(p))
		}
	}
	return n, nil
}
func parseSuccess(m message) ([16]byte, error) {
	var token [16]byte
	b := m.body
	switch m.code {
	case 7, 5:
		if len(b) < 3 || int(b[2]) != len(b)-3 {
			return token, ErrProtocol
		}
		b = b[3:]
		if m.code == 7 {
			opts, err := parseOptions(b, false)
			if err != nil {
				return token, err
			}
			b = opts[2]
		}
	case 3:
	default:
		return token, ErrProtocol
	}
	if len(b) != 16 {
		return token, ErrProtocol
	}
	copy(token[:], b)
	return token, nil
}
func addrOption(ap netip.AddrPort) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint16(b, 2)
	binary.BigEndian.PutUint16(b[2:], ap.Port())
	ip := ap.Addr().As4()
	copy(b[4:], ip[:])
	return b
}
func sessionInit(o Options, token [16]byte, mask [8]byte, source, target netip.AddrPort) []byte {
	b := option(1, token[:], false)
	if target.IsValid() {
		b = append(b, option(2, addrOption(target), false)...)
	}
	b = append(b, option(7, be16(uint16(o.mtu())), false)...)
	b = append(b, option(10, []byte(o.dataArea()), false)...)
	if target.IsValid() {
		b = append(b, option(2, addrOption(target), false)...)
	}
	b = append(b, option(3, addrOption(source), false)...)
	kind := byte(17)
	if target.IsValid() {
		kind = 8
	}
	b = append(b, option(kind, mask[:], false)...)
	out := append(append([]byte{}, dataMagic...), be16(0x8000|uint16(len(b)))...)
	return append(out, b...)
}
func checkACK(b []byte) error {
	if len(b) != 14 || b[0] != 5 {
		return ErrProtocol
	}
	if b[1] == 3 {
		return ErrSession
	}
	if b[1] != 0 {
		return ErrProtocol
	}
	return nil
}
func xor(b []byte, mask [8]byte, offset uint64) {
	for i := range b {
		b[i] ^= mask[(offset+uint64(i))&7]
	}
}
func udpFrame(target netip.AddrPort, b []byte, mask [8]byte) []byte {
	out := make([]byte, 10+len(b))
	out[1] = 6
	binary.BigEndian.PutUint16(out[4:], target.Port())
	ip := target.Addr().As4()
	copy(out[6:], ip[:])
	copy(out[10:], b)
	xor(out[10:], mask, 0)
	return out
}
func parseUDP(b []byte, mask [8]byte) (netip.AddrPort, []byte, error) {
	if len(b) < 10 || b[0] != 0 || b[1] != 6 {
		return netip.AddrPort{}, nil, ErrProtocol
	}
	var ip [4]byte
	copy(ip[:], b[6:10])
	ap := netip.AddrPortFrom(netip.AddrFrom4(ip), binary.BigEndian.Uint16(b[4:]))
	if ap.Port() == 0 {
		return ap, nil, ErrProtocol
	}
	xor(b[10:], mask, 0)
	return ap, b[10:], nil
}
