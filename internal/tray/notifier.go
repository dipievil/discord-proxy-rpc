package tray

import "errors"

// ErrUnsupportedPlatform is returned by NewNotifier where no tray backend
// exists. The tray targets Linux desktops through a D-Bus StatusNotifierItem;
// other platforms only get a compiling stub.
var ErrUnsupportedPlatform = errors.New("tray indicator is not supported on this platform yet")

// Notifier renders the tray item. Set is called on every status transition and
// Close releases the backend's resources.
type Notifier interface {
	Set(status Status) error
	Close() error
}
