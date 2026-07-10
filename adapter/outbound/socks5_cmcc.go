package outbound

import (
	"fmt"
	"strings"

	"github.com/metacubex/mihomo/transport/socks5"
)

func parseCMCCAuthMethod(value string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "0x00":
		return 0, nil
	case "0x80", "80", "128":
		return socks5.CMCCAuthMethod80, nil
	case "0x82", "82", "130":
		return socks5.CMCCAuthMethod82, nil
	default:
		return 0, fmt.Errorf("unsupported cmcc-auth-method %q; expected 0x80 or 0x82", value)
	}
}

func validateCMCCCredentials(user *socks5.User) error {
	if user.Username == "" {
		return fmt.Errorf("cmcc-auth-method requires a username")
	}
	if len(user.Username) > socks5.MaxAuthLen {
		return fmt.Errorf("CMCC SOCKS5 username is too long: %d bytes", len(user.Username))
	}
	if user.Password == "" {
		return fmt.Errorf("cmcc-auth-method requires a password")
	}
	return nil
}
