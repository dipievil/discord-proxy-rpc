//go:build linux

package tray

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSNIProperties(t *testing.T) {
	props := sniProperties(StatusFor(StateConnected))
	item, ok := props[sniInterface]
	if !ok {
		t.Fatalf("properties have no %s interface", sniInterface)
	}

	for _, name := range []string{"Category", "Id", "Status", "IconPixmap", "ToolTip", "ItemIsMenu", "Menu"} {
		if _, ok := item[name]; !ok {
			t.Errorf("property %q is missing", name)
		}
	}

	if got := item["Category"].Value; got != "ApplicationStatus" {
		t.Errorf("Category = %v, want ApplicationStatus", got)
	}
	if got := item["Status"].Value; got != activeStatus {
		t.Errorf("Status = %v, want %s", got, activeStatus)
	}
	if got := item["Menu"].Value; got != dbus.ObjectPath("/NO_DBUSMENU") {
		t.Errorf("Menu = %v, want /NO_DBUSMENU", got)
	}

	pixmaps, ok := item["IconPixmap"].Value.([]Pixmap)
	if !ok {
		t.Fatalf("IconPixmap has type %T, want []Pixmap", item["IconPixmap"].Value)
	}
	if len(pixmaps) != len(iconSizes) {
		t.Errorf("IconPixmap has %d entries, want %d", len(pixmaps), len(iconSizes))
	}
	if len(pixmaps) > 0 && len(pixmaps[0].Data) == 0 {
		t.Error("IconPixmap data is empty")
	}
}

func TestSNIToolTip(t *testing.T) {
	status := StatusFor(StateReconnecting)
	tooltip := sniToolTipFor(status)

	if tooltip.Title != status.Tooltip {
		t.Errorf("ToolTip title = %q, want %q", tooltip.Title, status.Tooltip)
	}
	if tooltip.IconName != "" {
		t.Errorf("ToolTip icon name = %q, want empty (pixmaps only)", tooltip.IconName)
	}
	if len(tooltip.IconPixmap) != len(iconSizes) {
		t.Errorf("ToolTip has %d pixmaps, want %d", len(tooltip.IconPixmap), len(iconSizes))
	}
	if string(tooltip.IconPixmap[0].Data) != string(Pixmaps(status.Icon)[0].Data) {
		t.Error("ToolTip icon does not match the status icon")
	}
}

func TestSNIPropertiesFollowStatus(t *testing.T) {
	disconnected := sniProperties(StatusFor(StateDisconnected))[sniInterface]
	connected := sniProperties(StatusFor(StateConnected))[sniInterface]
	disconnectedPixmaps := disconnected["IconPixmap"].Value.([]Pixmap)
	connectedPixmaps := connected["IconPixmap"].Value.([]Pixmap)
	if string(disconnectedPixmaps[0].Data) == string(connectedPixmaps[0].Data) {
		t.Error("connected and disconnected property icons are identical")
	}
}
