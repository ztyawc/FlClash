package updater

import "errors"

// checkCoreUpdateAllowed keeps the built-in updater from replacing this fork's
// kernel with an official build that cannot speak CMCC SOCKS5.
func checkCoreUpdateAllowed() error {
	return errors.New("update error: core upgrade is disabled for CMCC kernels, download new kernels from https://github.com/ztyawc/mihomo/releases")
}
