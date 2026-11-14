//go:build !linux

package tray

// NewNotifier reports that this platform has no tray backend. Other GOOSes
// keep compiling so `go build ./...` and the cross-compiles stay green.
func NewNotifier() (Notifier, error) {
	return nil, ErrUnsupportedPlatform
}
