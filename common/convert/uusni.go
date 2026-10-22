package convert

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/transport/uusni"
)

// parseUUSNI accepts the links exported by the parameter capture module and
// v2rayNG. Unsupported or ambiguous variants must not become another protocol.
func parseUUSNI(line string) (map[string]any, error) {
	u, err := url.Parse(line)
	if err != nil || !strings.EqualFold(u.Scheme, "uusni") || u.User != nil || u.Path != "" || u.Opaque != "" {
		return nil, errors.New("invalid uusni URI")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, err
	}
	for key, values := range query {
		if len(values) != 1 {
			return nil, errors.New("duplicate uusni parameter")
		}
		switch key {
		case "obfs-key", "sni-crypt", "custom-sni", "outer-tls":
		default:
			return nil, errors.New("unsupported uusni parameter")
		}
	}
	key, err := strconv.ParseUint(query.Get("obfs-key"), 10, 8)
	if err != nil {
		return nil, errors.New("uusni URI requires obfs-key in 0..255")
	}
	crypt, err := strconv.ParseBool(query.Get("sni-crypt"))
	if err != nil {
		return nil, errors.New("uusni URI requires sni-crypt")
	}
	if value, exists := query["custom-sni"]; exists {
		custom, err := strconv.ParseBool(value[0])
		if err != nil || !custom {
			return nil, errors.New("uusni requires the custom protocol")
		}
	}
	if value, exists := query["outer-tls"]; exists {
		tls, err := strconv.ParseBool(value[0])
		if err != nil || tls {
			return nil, errors.New("uusni does not support outer TLS")
		}
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		return nil, errors.New("invalid uusni port")
	}
	config := uusni.Config{Server: u.Hostname(), Port: int(port), ObfsKey: int(key), SNICrypt: crypt}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	name := u.Fragment
	if name == "" {
		name = "UU SNI"
	}
	return map[string]any{"name": name, "type": "uusni", "server": config.Server, "port": config.Port, "obfs-key": config.ObfsKey, "sni-crypt": crypt, "udp": false}, nil
}
