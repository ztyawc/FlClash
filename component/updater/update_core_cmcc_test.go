package updater

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoreUpdateIsRefusedForCMCCKernels(t *testing.T) {
	// A missing path proves the guard runs first: without it Update would fail
	// on os.Stat instead, and never touches the network or any file.
	err := DefaultCoreUpdater.Update("/nonexistent/mihomo", ReleaseChannel, true)
	require.ErrorContains(t, err, "core upgrade is disabled for CMCC kernels")
}
