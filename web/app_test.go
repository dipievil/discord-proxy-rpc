package web

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// runDashboardJS loads web/app.js into a goja VM with a minimal browser mock
// so the dashboard's pure helper functions can be exercised.
func runDashboardJS(t *testing.T) *goja.Runtime {
	t.Helper()

	data, err := fs.ReadFile(FS, "app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js): %v", err)
	}

	vm := goja.New()

	// Minimal DOM mocks so the IIFE can initialize without a real browser.
	document := vm.NewObject()
	document.Set("readyState", "complete")
	document.Set("getElementById", func(string) goja.Value { return goja.Null() })
	document.Set("querySelector", func(string) goja.Value { return goja.Null() })
	document.Set("addEventListener", func(...interface{}) {})
	document.Set("createElement", func(string) goja.Value { return vm.NewObject() })
	vm.Set("document", document)

	location := vm.NewObject()
	location.Set("protocol", "http:")
	location.Set("host", "localhost")
	vm.Set("location", location)

	window := vm.NewObject()
	window.Set("location", location)
	vm.Set("window", window)

	vm.Set("navigator", vm.NewObject())
	vm.Set("console", map[string]interface{}{
		"log":   func(...interface{}) {},
		"warn":  func(...interface{}) {},
		"error": func(...interface{}) {},
	})
	vm.Set("WebSocket", func(goja.ConstructorCall) *goja.Object { return nil })

	// Minimal URL parser used by isSafeImageUrl.
	vm.Set("URL", func(call goja.ConstructorCall) *goja.Object {
		raw := call.Argument(0).String()
		obj := vm.NewObject()

		scheme := "http"
		rest := raw
		if idx := strings.Index(raw, "://"); idx >= 0 {
			scheme = raw[:idx]
			rest = raw[idx+3:]
		}

		host := rest
		if idx := strings.Index(rest, "/"); idx >= 0 {
			host = rest[:idx]
		}

		obj.Set("protocol", scheme+":")
		obj.Set("hostname", host)
		return obj
	})

	if _, err := vm.RunString(string(data)); err != nil {
		t.Fatalf("run app.js: %v", err)
	}

	return vm
}

func dashboardAPI(t *testing.T, vm *goja.Runtime) *goja.Object {
	t.Helper()

	window := vm.Get("window").ToObject(vm)
	api := window.Get("__dashboard")
	if api == nil || goja.IsUndefined(api) || goja.IsNull(api) {
		t.Fatal("window.__dashboard not exposed by app.js")
	}
	return api.ToObject(vm)
}

func dashboardFn(t *testing.T, vm *goja.Runtime, name string) goja.Callable {
	t.Helper()

	fn, ok := goja.AssertFunction(dashboardAPI(t, vm).Get(name))
	if !ok {
		t.Fatalf("%s is not callable", name)
	}
	return fn
}

func TestDashboardCopyShortcut(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "isCopyShortcut")

	tests := []struct {
		name string
		evt  map[string]interface{}
		want bool
	}{
		{
			name: "ctrl+alt+c copies",
			evt:  map[string]interface{}{"ctrlKey": true, "altKey": true, "metaKey": false, "shiftKey": false, "code": "KeyC"},
			want: true,
		},
		{
			name: "cmd+alt+c copies",
			evt:  map[string]interface{}{"ctrlKey": false, "altKey": true, "metaKey": true, "shiftKey": false, "code": "KeyC"},
			want: true,
		},
		{
			name: "ctrl+shift+c no longer triggers (browser DevTools)",
			evt:  map[string]interface{}{"ctrlKey": true, "altKey": false, "metaKey": false, "shiftKey": true, "code": "KeyC"},
			want: false,
		},
		{
			name: "no modifiers ignored",
			evt:  map[string]interface{}{"ctrlKey": false, "altKey": false, "metaKey": false, "shiftKey": false, "code": "KeyC"},
			want: false,
		},
		{
			name: "different key ignored",
			evt:  map[string]interface{}{"ctrlKey": true, "altKey": true, "metaKey": false, "shiftKey": false, "code": "KeyV"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fn(goja.Undefined(), vm.ToValue(tt.evt))
			if err != nil {
				t.Fatalf("isCopyShortcut call: %v", err)
			}
			if got.ToBoolean() != tt.want {
				t.Errorf("isCopyShortcut(%v) = %v, want %v", tt.evt, got.ToBoolean(), tt.want)
			}
		})
	}
}

func TestDashboardResolveAssetSrc(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "resolveAssetSrc")

	tests := []struct {
		name     string
		imageID  string
		clientID string
		want     string
	}{
		{
			name:     "asset id with client id resolves to CDN",
			imageID:  "12345",
			clientID: "client-1",
			want:     "https://cdn.discordapp.com/app-assets/client-1/12345.png",
		},
		{
			name:     "safe https CDN url passes through",
			imageID:  "https://cdn.discordapp.com/emojis/987.png",
			clientID: "client-1",
			want:     "https://cdn.discordapp.com/emojis/987.png",
		},
		{
			name:     "safe http CDN url passes through",
			imageID:  "http://cdn.discordapp.com/emojis/987.png",
			clientID: "client-1",
			want:     "http://cdn.discordapp.com/emojis/987.png",
		},
		{
			name:     "external url is rejected (placeholder shown)",
			imageID:  "http://evil.example.com/x.png",
			clientID: "client-1",
			want:     "",
		},
		{
			name:     "empty image id yields no source (placeholder shown)",
			imageID:  "",
			clientID: "client-1",
			want:     "",
		},
		{
			name:     "asset id without client id yields no source (placeholder shown)",
			imageID:  "12345",
			clientID: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fn(goja.Undefined(), vm.ToValue(tt.imageID), vm.ToValue(tt.clientID))
			if err != nil {
				t.Fatalf("resolveAssetSrc call: %v", err)
			}

			if tt.want == "" {
				if !goja.IsNull(got) {
					t.Errorf("resolveAssetSrc(%q, %q) = %q, want null", tt.imageID, tt.clientID, got.String())
				}
				return
			}

			if got.String() != tt.want {
				t.Errorf("resolveAssetSrc(%q, %q) = %q, want %q", tt.imageID, tt.clientID, got.String(), tt.want)
			}
		})
	}
}

func TestDashboardFormatDuration(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "formatDuration")

	tests := []struct {
		seconds int64
		want    string
	}{
		{0, "0s"},
		{59, "59s"},
		{65, "1m 5s"},
		{3661, "1h 1m 1s"},
	}

	for _, tt := range tests {
		got, err := fn(goja.Undefined(), vm.ToValue(tt.seconds))
		if err != nil {
			t.Fatalf("formatDuration call: %v", err)
		}
		if got.String() != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.seconds, got.String(), tt.want)
		}
	}
}