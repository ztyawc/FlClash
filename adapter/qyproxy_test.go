package adapter

import (
	"encoding/json"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/stretchr/testify/require"
)

func TestParseQyProxy(t *testing.T) {
	for _, role := range []string{"primary", "cn2", "cn2_download"} {
		m := map[string]any{"type": "qyproxy", "name": "test", "server": "192.0.2.1", "port": 7025, "role": role, "username": "synthetic-user", "password": "synthetic-password", "sn": "synthetic-device", "game-id": 123, "server-id": 456}
		p, e := ParseProxy(m)
		require.NoError(t, e)
		require.Equal(t, C.QyProxy, p.Type())
		require.True(t, p.SupportUDP())
		b, e := json.Marshal(p)
		require.NoError(t, e)
		require.NotContains(t, string(b), "synthetic")
		require.NoError(t, p.Close())
		m["role"] = "qyvpn"
		_, e = ParseProxy(m)
		require.Error(t, e)
	}
}
