package adapter

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/metacubex/mihomo/common/convert"
	C "github.com/metacubex/mihomo/constant"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func uusniMapping() map[string]any {
	return map[string]any{"type": "uusni", "name": "UU test", "server": "203.0.113.10", "port": 5556, "obfs-key": 77, "sni-crypt": true}
}

func TestParseUUSNIYAMLAndURI(t *testing.T) {
	var m map[string]any
	require.NoError(t, yaml.Unmarshal([]byte("type: uusni\nname: UU test\nserver: 203.0.113.10\nport: 5556\nobfs-key: 77\nsni-crypt: true\nudp: false\n"), &m))
	for _, key := range []any{0, 255, "77", float64(77), json.Number("77")} {
		m["obfs-key"] = key
		p, err := ParseProxy(m)
		require.NoError(t, err)
		require.Equal(t, C.UUSNI, p.Type())
		require.Equal(t, "UUSNI", p.Type().String())
		require.False(t, p.SupportUDP())
		require.False(t, p.SupportUOT())
		status, err := p.MarshalJSON()
		require.NoError(t, err)
		require.NotContains(t, string(status), "203.0.113.10")
		require.NotContains(t, string(status), "obfs-key")
		require.NoError(t, p.Close())
	}
	links, err := convert.ConvertsV2Ray([]byte("uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1#UU%20SNI"))
	require.NoError(t, err)
	require.Len(t, links, 1)
	p, err := ParseProxy(links[0])
	require.NoError(t, err)
	require.Equal(t, C.UUSNI, p.Type())
	require.NoError(t, p.Close())
}

func TestParseUUSNIRejectsUnsupportedOptions(t *testing.T) {
	for _, field := range []string{"obfs-key", "sni-crypt", "port", "server"} {
		m := uusniMapping()
		delete(m, field)
		_, err := ParseProxy(m)
		require.Error(t, err, "missing "+field)
	}
	for _, key := range []any{nil, -1, 256, 77.5, math.NaN(), math.Inf(1), true, "", "-1", "256", "1.5", []any{77}} {
		m := uusniMapping()
		m["obfs-key"] = key
		_, err := ParseProxy(m)
		require.Error(t, err, "invalid XOR %v", key)
	}
	for _, port := range []any{nil, 0, -1, 65536, 5556.5, "1.5"} {
		m := uusniMapping()
		m["port"] = port
		_, err := ParseProxy(m)
		require.Error(t, err)
	}
	for field, value := range map[string]any{"udp": true, "tls": true, "outer-tls": true, "custom-sni": false, "network": "ws", "sni-crypt": 2, "server": "node.example", "ip-version": "ipv6", "smux": map[string]any{"enabled": true}} {
		m := uusniMapping()
		m[field] = value
		_, err := ParseProxy(m)
		require.Error(t, err, "unsupported %s", field)
	}
	// Normalized keys accept the field names used in captured portable configs.
	m := uusniMapping()
	delete(m, "obfs-key")
	m["obfs_key"] = 77
	m["outer_tls"] = false
	m["custom_sni"] = true
	p, err := ParseProxy(m)
	require.NoError(t, err)
	require.NoError(t, p.Close())
	// Existing protocol type numbers are not renumbered by the appended type.
	require.Equal(t, C.QyProxy+1, C.UUSNI)
	require.Equal(t, "QyProxy", C.QyProxy.String())
	require.False(t, strings.Contains(C.UUSNI.String(), "Socks"))
}
