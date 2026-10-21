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

	// Minimal URLSearchParams used by resolveToken for the ?token= query param.
	vm.Set("URLSearchParams", func(call goja.ConstructorCall) *goja.Object {
		query := strings.TrimPrefix(call.Argument(0).String(), "?")

		params := map[string]string{}
		for _, pair := range strings.Split(query, "&") {
			if pair == "" {
				continue
			}
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				params[kv[0]] = kv[1]
			} else {
				params[kv[0]] = ""
			}
		}

		obj := vm.NewObject()
		obj.Set("get", func(key string) goja.Value {
			if v, ok := params[key]; ok {
				return vm.ToValue(v)
			}
			return goja.Null()
		})
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

func TestDashboardGetWsPath(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "getWsPath")

	got, err := fn(goja.Undefined())
	if err != nil {
		t.Fatalf("getWsPath call: %v", err)
	}
	if got.String() != "/ws" {
		t.Errorf("getWsPath() = %q, want %q (default when no meta tag)", got.String(), "/ws")
	}

	doc := vm.Get("document").ToObject(vm)
	meta := vm.NewObject()
	meta.Set("content", "/custom-ws")
	doc.Set("querySelector", func(string) goja.Value { return meta })

	got, err = fn(goja.Undefined())
	if err != nil {
		t.Fatalf("getWsPath call: %v", err)
	}
	if got.String() != "/custom-ws" {
		t.Errorf("getWsPath() = %q, want %q (from injected meta tag)", got.String(), "/custom-ws")
	}
}

func TestDashboardBuildSubprotocols(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "buildSubprotocols")

	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "no token yields no subprotocols", token: "", want: ""},
		{name: "token base64url encoded into subprotocol", token: "abc", want: "proxy-token-YWJj"},
		{name: "token with utf-8 bytes", token: "tôkén", want: "proxy-token-dMO0a8Opbg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fn(goja.Undefined(), vm.ToValue(tt.token))
			if err != nil {
				t.Fatalf("buildSubprotocols call: %v", err)
			}

			arr := got.ToObject(vm)
			if arr.Get("length").ToInteger() == 0 {
				if tt.want != "" {
					t.Errorf("buildSubprotocols(%q) = empty, want %q", tt.token, tt.want)
				}
				return
			}

			first := arr.Get("0").String()
			if first != tt.want {
				t.Errorf("buildSubprotocols(%q) = %q, want %q", tt.token, first, tt.want)
			}
		})
	}
}

func TestDashboardComputeTimestamps(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "computeTimestamps")

	// Discord IPC timestamps are Unix milliseconds.
	const now = int64(1700000000000)

	run := func(name string, values map[string]int64, wantElapsed, wantRemaining interface{}, wantEnded bool) {
		t.Run(name, func(t *testing.T) {
			ts := vm.NewObject()
			for key, value := range values {
				ts.Set(key, value)
			}

			got, err := fn(goja.Undefined(), ts, vm.ToValue(now))
			if err != nil {
				t.Fatalf("computeTimestamps call: %v", err)
			}

			obj := got.ToObject(vm)
			assertNum := func(key string, want interface{}) {
				v := obj.Get(key)
				switch w := want.(type) {
				case nil:
					if !goja.IsNull(v) && !goja.IsUndefined(v) {
						t.Errorf("%s = %v, want null", key, v)
					}
				case int64:
					if v.ToInteger() != w {
						t.Errorf("%s = %d, want %d", key, v.ToInteger(), w)
					}
				}
			}

			assertNum("elapsed", wantElapsed)
			assertNum("remaining", wantRemaining)
			if obj.Get("ended").ToBoolean() != wantEnded {
				t.Errorf("ended = %v, want %v", obj.Get("ended").ToBoolean(), wantEnded)
			}
		})
	}

	run("start 65 seconds ago", map[string]int64{"start": now - 65000}, int64(65), nil, false)
	run("end in one hour", map[string]int64{"end": now + 3600000}, nil, int64(3600), false)
	run("start and end", map[string]int64{"start": now, "end": now + 60000}, int64(0), int64(60), false)
	run("end already passed", map[string]int64{"end": now - 1000}, nil, nil, true)
	run("start in the future", map[string]int64{"start": now + 5000}, nil, nil, false)
	run("no timestamps", map[string]int64{}, nil, nil, false)
}

func TestDashboardResolveTokenFromUrl(t *testing.T) {
	vm := runDashboardJS(t)

	location := vm.Get("location").ToObject(vm)
	location.Set("search", "?token=url-token")

	window := vm.Get("window").ToObject(vm)

	store := map[string]string{}
	sessionStorage := vm.NewObject()
	sessionStorage.Set("getItem", func(key string) goja.Value {
		if v, ok := store[key]; ok {
			return vm.ToValue(v)
		}
		return goja.Null()
	})
	sessionStorage.Set("setItem", func(key, value string) {
		store[key] = value
	})
	window.Set("sessionStorage", sessionStorage)

	fn := dashboardFn(t, vm, "resolveToken")

	got, err := fn(goja.Undefined())
	if err != nil {
		t.Fatalf("resolveToken call: %v", err)
	}
	if got.String() != "url-token" {
		t.Errorf("resolveToken() = %q, want %q", got.String(), "url-token")
	}

	if stored := store["proxy-rpc-token"]; stored != "url-token" {
		t.Errorf("stored token = %q, want %q", stored, "url-token")
	}
}

func TestDashboardPresenceView(t *testing.T) {
	vm := runDashboardJS(t)
	fn := dashboardFn(t, vm, "presenceView")

	tests := []struct {
		name            string
		payload         map[string]interface{}
		wantSource      string
		wantHasActivity bool
		wantDetails     string
	}{
		{
			name:       "no source",
			payload:    map[string]interface{}{"source": "none", "activity": nil},
			wantSource: "none",
		},
		{
			name:            "ipc source with activity",
			payload:         map[string]interface{}{"source": "ipc", "activity": map[string]interface{}{"details": "Playing"}},
			wantSource:      "ipc",
			wantHasActivity: true,
			wantDetails:     "Playing",
		},
		{
			name:       "ipc source without activity",
			payload:    map[string]interface{}{"source": "ipc", "activity": nil},
			wantSource: "ipc",
		},
		{
			name:       "missing source is treated as no source",
			payload:    map[string]interface{}{"activity": map[string]interface{}{"details": "Playing"}},
			wantSource: "none",
		},
		{
			name:       "empty payload",
			payload:    map[string]interface{}{},
			wantSource: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fn(goja.Undefined(), vm.ToValue(tt.payload))
			if err != nil {
				t.Fatalf("presenceView call: %v", err)
			}

			obj := got.ToObject(vm)
			if src := obj.Get("source").String(); src != tt.wantSource {
				t.Errorf("source = %q, want %q", src, tt.wantSource)
			}
			if has := obj.Get("hasActivity").ToBoolean(); has != tt.wantHasActivity {
				t.Errorf("hasActivity = %v, want %v", has, tt.wantHasActivity)
			}

			activity := obj.Get("activity")
			if tt.wantHasActivity {
				if details := activity.ToObject(vm).Get("details").String(); details != tt.wantDetails {
					t.Errorf("activity.details = %q, want %q", details, tt.wantDetails)
				}
			} else if !goja.IsNull(activity) {
				t.Errorf("activity = %v, want null", activity)
			}
		})
	}
}
