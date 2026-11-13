package adapter

import (
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func vks6Mapping() map[string]any {
	return map[string]any{"type": "vk-s6", "name": "VK download", "server": "203.0.113.10",
		"port": 8050, "port-end": 8150, "username": "vk-test", "password": "synthetic-secret"}
}

func TestParseVKS6(t *testing.T) {
	var mapping map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(`type: vk-s6
name: VK download
server: 203.0.113.10
port: 8050
port-end: 8150
username: vk-test
password: synthetic-secret
udp: false
`), &mapping))
	p, err := ParseProxy(mapping)
	require.NoError(t, err)
	require.Equal(t, C.VKS6, p.Type())
	require.Equal(t, "VKS6", p.Type().String())
	require.False(t, p.SupportUDP())
	require.False(t, p.SupportUOT())
	status, err := p.MarshalJSON()
	require.NoError(t, err)
	require.NotContains(t, string(status), "synthetic-secret")
	require.NoError(t, p.Close())
	// A single fixed TCP port is valid when port-end is omitted.
	delete(mapping, "port-end")
	p, err = ParseProxy(mapping)
	require.NoError(t, err)
	require.NoError(t, p.Close())
}

func TestParseVKS6RejectsUnsupportedSettings(t *testing.T) {
	for _, field := range []string{"server", "port", "username", "password"} {
		mapping := vks6Mapping()
		delete(mapping, field)
		_, err := ParseProxy(mapping)
		require.Error(t, err, "missing "+field)
	}
	for field, value := range map[string]any{
		"server": "node.example", "port": 0, "port-end": 8049,
		"udp": true, "tls": true, "network": "ws", "ip-version": "ipv6",
		"smux": map[string]any{"enabled": true},
	} {
		mapping := vks6Mapping()
		mapping[field] = value
		_, err := ParseProxy(mapping)
		require.Error(t, err, "unsupported "+field)
	}
	for _, value := range []any{8050.5, "8050.5", -1, 65536} {
		mapping := vks6Mapping()
		mapping["port"] = value
		_, err := ParseProxy(mapping)
		require.Error(t, err, "invalid port %v", value)
	}
	for _, value := range []any{8150.5, "8150.5", -1, 65536} {
		mapping := vks6Mapping()
		mapping["port-end"] = value
		_, err := ParseProxy(mapping)
		require.Error(t, err, "invalid port-end %v", value)
	}
	require.False(t, strings.Contains(C.VKS6.String(), "Socks"))
}
