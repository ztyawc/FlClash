package convert

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUUSNIURIConversion(t *testing.T) {
	input := "uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1#测试游戏\n" +
		"uusni://203.0.113.10:5556?obfs-key=0&sni-crypt=0#测试游戏"
	for _, data := range [][]byte{[]byte(input), []byte(base64.StdEncoding.EncodeToString([]byte(input)))} {
		proxies, err := ConvertsV2Ray(data)
		require.NoError(t, err)
		require.Len(t, proxies, 2)
		require.Equal(t, "uusni", proxies[0]["type"])
		require.Equal(t, "测试游戏", proxies[0]["name"])
		require.NotEqual(t, proxies[0]["name"], proxies[1]["name"])
		require.Equal(t, 77, proxies[0]["obfs-key"])
		require.Equal(t, true, proxies[0]["sni-crypt"])
		require.Equal(t, 0, proxies[1]["obfs-key"])
		require.Equal(t, false, proxies[1]["sni-crypt"])
		require.Equal(t, false, proxies[0]["udp"])
	}
	p, err := parseUUSNI("uusni://203.0.113.10:5556?obfs-key=255&sni-crypt=true&outer-tls=0&custom-sni=1")
	require.NoError(t, err)
	require.Equal(t, "UU SNI", p["name"])
}

func TestUUSNIURIRejectsIncompleteAndUnsupportedVariants(t *testing.T) {
	for _, uri := range []string{
		"uusni://203.0.113.10:5556?sni-crypt=1",
		"uusni://203.0.113.10:5556?obfs-key=77",
		"uusni://203.0.113.10:5556?obfs-key=256&sni-crypt=1",
		"uusni://203.0.113.10:5556?obfs-key=77.5&sni-crypt=1",
		"uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=2",
		"uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1&outer-tls=1",
		"uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1&custom-sni=0",
		"uusni://203.0.113.10:5556?obfs-key=77&obfs-key=66&sni-crypt=1",
		"uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1&tls=1",
		"uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1&extra=%ZZ",
		"uusni://user:password@203.0.113.10:5556?obfs-key=77&sni-crypt=1",
		"uusni://203.0.113.10:5556/path?obfs-key=77&sni-crypt=1",
		"uusni://node.example:5556?obfs-key=77&sni-crypt=1",
		"uusni://[::1]:5556?obfs-key=77&sni-crypt=1",
		"uusni://203.0.113.10:0?obfs-key=77&sni-crypt=1",
	} {
		_, err := parseUUSNI(uri)
		require.Error(t, err, uri)
	}
}
