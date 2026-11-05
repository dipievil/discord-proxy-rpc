package tray

import (
	"strings"
	"testing"
)

// TestErrUnsupportedPlatformMessage pins the wording every non-Linux build
// prints: the stub exits with this error, so the message must stay explicit.
func TestErrUnsupportedPlatformMessage(t *testing.T) {
	if got := ErrUnsupportedPlatform.Error(); !strings.Contains(got, "not supported on this platform yet") {
		t.Errorf("ErrUnsupportedPlatform = %q, want it to say the platform is not supported yet", got)
	}
}
