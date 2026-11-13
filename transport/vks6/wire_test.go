package vks6

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{Server: "203.0.113.10", Port: 8050, PortEnd: 8150,
		Username: "vk-test", Password: "0123456789abcdef"}
}

func TestCapturedFormatVector(t *testing.T) {
	target := netip.MustParseAddrPort("43.174.102.54:80")
	got, err := Header(testConfig(), target)
	if err != nil {
		t.Fatal(err)
	}
	// Independent Python struct/socket calculation of the 34-byte format.
	want, _ := hex.DecodeString("06074a4d6917295837100c177650781e7577041f25012f4f262609012bae66360050")
	if !bytes.Equal(got, want) {
		t.Fatalf("header %x, want %x", got, want)
	}
}

func TestHeaderRejectsUnsupportedInputs(t *testing.T) {
	base := testConfig()
	for _, invalid := range []Config{
		{Server: "node.example", Port: 8050, PortEnd: 8150, Username: base.Username, Password: base.Password},
		{Server: "::1", Port: 8050, PortEnd: 8150, Username: base.Username, Password: base.Password},
		{Server: "0.0.0.0", Port: 8050, PortEnd: 8150, Username: base.Username, Password: base.Password},
		{Server: base.Server, Port: 0, PortEnd: 8150, Username: base.Username, Password: base.Password},
		{Server: base.Server, Port: 8151, PortEnd: 8150, Username: base.Username, Password: base.Password},
		{Server: base.Server, Port: 8050, PortEnd: 65536, Username: base.Username, Password: base.Password},
		{Server: base.Server, Port: 8050, PortEnd: 8150, Username: "", Password: base.Password},
		{Server: base.Server, Port: 8050, PortEnd: 8150, Username: base.Username, Password: strings.Repeat("x", 256)},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted invalid config: server=%q ports=%d-%d user_len=%d pass_len=%d",
				invalid.Server, invalid.Port, invalid.PortEnd, len(invalid.Username), len(invalid.Password))
		}
	}
	for _, target := range []netip.AddrPort{
		netip.MustParseAddrPort("[2001:db8::1]:443"),
		netip.MustParseAddrPort("43.174.102.54:0"),
	} {
		if _, err := Header(base, target); err == nil {
			t.Fatalf("accepted invalid target %s", target)
		}
	}
}
