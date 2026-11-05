//go:build linux

package tray

import (
	"errors"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	sniInterface     = "org.kde.StatusNotifierItem"
	sniPath          = dbus.ObjectPath("/StatusNotifierItem")
	watcherService   = "org.kde.StatusNotifierWatcher"
	watcherPath      = dbus.ObjectPath("/StatusNotifierWatcher")
	watcherInterface = "org.kde.StatusNotifierWatcher"
	itemID           = "discord-proxy-tray"
	activeStatus     = "Active"
)

// sniToolTip is the D-Bus signature the StatusNotifierItem spec requires for
// its ToolTip property: (sa(iiay)ss).
type sniToolTip struct {
	IconName   string
	IconPixmap []Pixmap
	Title      string
	SubTitle   string
}

// sniItem carries the methods the StatusNotifierItem interface declares. They
// are kept on a dedicated type so only they are exported on the bus; the
// notifier itself stays a plain Go value.
type sniItem struct{}

// Activate is called when the user clicks the icon. There is nothing to open
// yet, so it is a no-op.
func (sniItem) Activate(x, y int32) *dbus.Error { return nil }

// SecondaryActivate is the middle-click action.
func (sniItem) SecondaryActivate(x, y int32) *dbus.Error { return nil }

// ContextMenu is called for a right click; the tray exposes no menu.
func (sniItem) ContextMenu(x, y int32) *dbus.Error { return nil }

// Scroll is called when the icon is scrolled.
func (sniItem) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// sniNotifier publishes a StatusNotifierItem on the session bus and updates its
// icon and tooltip through D-Bus properties.
type sniNotifier struct {
	conn    *dbus.Conn
	service string
	props   *prop.Properties
}

// NewNotifier connects to the session bus, exports a StatusNotifierItem and
// registers it with the StatusNotifierWatcher. It fails when no session bus or
// no watcher is available (for example on a headless host).
func NewNotifier() (Notifier, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to the session bus: %w", err)
	}

	notifier := &sniNotifier{
		conn:    conn,
		service: fmt.Sprintf("%s-%d-1", sniInterface, os.Getpid()),
	}
	if err := notifier.export(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return notifier, nil
}

func (n *sniNotifier) export() error {
	// The well-known name is the conventional one; when another instance
	// already owns it, the connection's unique bus name works with the watcher
	// just as well.
	reply, err := n.conn.RequestName(n.service, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("requesting bus name %s: %w", n.service, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		if names := n.conn.Names(); len(names) > 0 {
			n.service = names[0]
		}
	}

	props, err := prop.Export(n.conn, sniPath, sniProperties(StatusFor(StateUnreachable)))
	if err != nil {
		return fmt.Errorf("exporting %s properties: %w", sniInterface, err)
	}
	n.props = props

	if err := n.conn.Export(sniItem{}, sniPath, sniInterface); err != nil {
		return fmt.Errorf("exporting %s: %w", sniInterface, err)
	}

	watcher := n.conn.Object(watcherService, watcherPath)
	if call := watcher.Call(watcherInterface+".RegisterStatusNotifierItem", 0, n.service); call.Err != nil {
		return fmt.Errorf("registering with %s: %w", watcherService, call.Err)
	}
	return nil
}

// setProperty updates an exported property and emits PropertiesChanged. The
// properties stay read-only on the bus (no peer can overwrite the icon), which
// is why the internal setter is used: godbus only exposes it as SetMust, which
// panics on failure, so the panic is converted into an error the poll loop can
// log and retry.
func (n *sniNotifier) setProperty(name string, value any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("setting %s: %v", name, recovered)
		}
	}()
	n.props.SetMust(sniInterface, name, value)
	return nil
}

// Set swaps the icon and tooltip for the new status and notifies listeners.
func (n *sniNotifier) Set(status Status) error {
	if err := n.setProperty("IconPixmap", Pixmaps(status.Icon)); err != nil {
		return err
	}
	if err := n.setProperty("ToolTip", sniToolTipFor(status)); err != nil {
		return err
	}

	var errs []error
	if err := n.conn.Emit(sniPath, sniInterface+".NewIcon"); err != nil {
		errs = append(errs, fmt.Errorf("emitting NewIcon: %w", err))
	}
	if err := n.conn.Emit(sniPath, sniInterface+".NewToolTip"); err != nil {
		errs = append(errs, fmt.Errorf("emitting NewToolTip: %w", err))
	}
	return errors.Join(errs...)
}

// Close drops the session-bus connection, which also unregisters the item.
func (n *sniNotifier) Close() error {
	return n.conn.Close()
}

// sniProperties is the initial StatusNotifierItem property set. It is pure
// (no bus involved), so the mapping is testable without D-Bus.
func sniProperties(status Status) prop.Map {
	return prop.Map{
		sniInterface: {
			"Category":            {Value: "ApplicationStatus", Emit: prop.EmitConst},
			"Id":                  {Value: itemID, Emit: prop.EmitConst},
			"Title":               {Value: trayTitle, Emit: prop.EmitConst},
			"Status":              {Value: activeStatus, Emit: prop.EmitTrue},
			"IconName":            {Value: "", Emit: prop.EmitFalse},
			"IconPixmap":          {Value: Pixmaps(status.Icon), Emit: prop.EmitTrue},
			"OverlayIconName":     {Value: "", Emit: prop.EmitFalse},
			"OverlayIconPixmap":   {Value: []Pixmap{}, Emit: prop.EmitFalse},
			"AttentionIconName":   {Value: "", Emit: prop.EmitFalse},
			"AttentionIconPixmap": {Value: []Pixmap{}, Emit: prop.EmitFalse},
			"AttentionMovieName":  {Value: "", Emit: prop.EmitFalse},
			"ToolTip":             {Value: sniToolTipFor(status), Emit: prop.EmitTrue},
			"ItemIsMenu":          {Value: false, Emit: prop.EmitConst},
			"Menu":                {Value: dbus.ObjectPath("/NO_DBUSMENU"), Emit: prop.EmitConst},
		},
	}
}

func sniToolTipFor(status Status) sniToolTip {
	return sniToolTip{
		IconPixmap: Pixmaps(status.Icon),
		Title:      status.Tooltip,
	}
}
