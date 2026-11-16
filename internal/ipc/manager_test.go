package ipc

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

func TestNewManagerClampsAccountCount(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "zero falls back to one", input: 0, want: 1},
		{name: "negative falls back to one", input: -3, want: 1},
		{name: "within the slot list", input: 3, want: 3},
		{name: "above the slot list is clamped", input: 10, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newManager("test-client-id", tt.input, []string{"0", "1", "2", "3"}, zap.NewNop())
			if m.maxAccounts != tt.want {
				t.Errorf("maxAccounts = %d, want %d", m.maxAccounts, tt.want)
			}
		})
	}
}

func TestManagerSingleAccountUsesFirstSlotThatConnects(t *testing.T) {
	good := newMockDiscordNamed(t, "discord-ipc-2")
	missing := filepath.Join(t.TempDir(), "discord-ipc-0")

	m := newManager("test-client-id", 1, []string{missing, good.path}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("Accounts() = %d entries, want 1", len(accounts))
	}
	if accounts[0].Name != "discord-ipc-2" {
		t.Errorf("account name = %q, want %q (identity from the connected slot)", accounts[0].Name, "discord-ipc-2")
	}
	if got := accounts[0].Client.State(); got != StateConnected {
		t.Errorf("account state = %v, want connected", got)
	}
	if m.Primary() != accounts[0] {
		t.Error("Primary() is not the only account")
	}
}

func TestManagerSingleAccountWithoutSocketKeepsFirstSlot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "discord-ipc-0")

	m := newManager("test-client-id", 1, []string{missing}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 1 {
		t.Fatalf("Accounts() = %d entries, want 1", len(accounts))
	}
	if accounts[0].Name != "discord-ipc-0" {
		t.Errorf("account name = %q, want %q", accounts[0].Name, "discord-ipc-0")
	}
	if got := accounts[0].Client.State(); got != StateDisconnected {
		t.Errorf("account state = %v, want disconnected", got)
	}
}

func TestManagerMultiAccountClaimsDistinctSlots(t *testing.T) {
	first := newMockDiscordNamed(t, "discord-ipc-0")
	second := newMockDiscordNamed(t, "discord-ipc-1")

	m := newManager("test-client-id", 2, []string{first.path, second.path}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("Accounts() = %d entries, want 2", len(accounts))
	}

	wantNames := []string{"discord-ipc-0", "discord-ipc-1"}
	for i, want := range wantNames {
		if accounts[i].Name != want {
			t.Errorf("account[%d] name = %q, want %q", i, accounts[i].Name, want)
		}
		if got := accounts[i].Client.State(); got != StateConnected {
			t.Errorf("account[%d] state = %v, want connected", i, got)
		}
	}

	if accounts[0].Client == accounts[1].Client {
		t.Error("accounts share one Client, want one client per account")
	}
	if accounts[0].Client.Socket() == accounts[1].Client.Socket() {
		t.Errorf("accounts share socket %q, want distinct slots", accounts[0].Client.Socket())
	}

	account, ok := m.Account("discord-ipc-1")
	if !ok {
		t.Fatal("Account(discord-ipc-1) not found")
	}
	if account != accounts[1] {
		t.Error("Account(discord-ipc-1) returned a different account")
	}
	if _, ok := m.Account("discord-ipc-9"); ok {
		t.Error("Account(discord-ipc-9) found an untracked account")
	}
}

func TestManagerKeepsSlotForAccountWithoutSocket(t *testing.T) {
	first := newMockDiscordNamed(t, "discord-ipc-0")
	missing := filepath.Join(t.TempDir(), "discord-ipc-1")

	m := newManager("test-client-id", 2, []string{first.path, missing}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("Accounts() = %d entries, want 2", len(accounts))
	}
	if accounts[0].Name != "discord-ipc-0" {
		t.Errorf("account[0] name = %q, want %q", accounts[0].Name, "discord-ipc-0")
	}
	if got := accounts[0].Client.State(); got != StateConnected {
		t.Errorf("account[0] state = %v, want connected", got)
	}
	if accounts[1].Name != "discord-ipc-1" {
		t.Errorf("account[1] name = %q, want the next free slot %q", accounts[1].Name, "discord-ipc-1")
	}
	if got := accounts[1].Client.State(); got != StateDisconnected {
		t.Errorf("account[1] state = %v, want disconnected until Discord appears", got)
	}
}

// A slot the proxy claimed before its Discord account was running must attach
// on the normal reconnect loop, without touching an account that is already
// connected.
func TestManagerOneAccountReconnectsWhileAnotherStaysConnected(t *testing.T) {
	first := newMockDiscordNamed(t, "discord-ipc-0")
	latePath := filepath.Join(t.TempDir(), "discord-ipc-1")

	m := newManager("test-client-id", 2, []string{first.path, latePath}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("Accounts() = %d entries, want 2", len(accounts))
	}
	secondary := accounts[1]
	if secondary.Name != "discord-ipc-1" {
		t.Fatalf("account[1] name = %q, want %q", secondary.Name, "discord-ipc-1")
	}
	secondary.Client.timeout = 150 * time.Millisecond

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 20 * time.Millisecond,
		MaxReconnectInterval:  50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- secondary.Client.RunWithReconnect(ctx, cfg) }()

	// The second account's Discord shows up only now, after the slot was claimed.
	newMockDiscordAt(t, latePath)

	waitForState(t, secondary.Client, StateConnected, 5*time.Second)
	if got := accounts[0].Client.State(); got != StateConnected {
		t.Errorf("account[0] state = %v, want connected while the other account reconnects", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("account[1] reconnect loop did not stop after cancellation")
	}
}

func TestManagerWithoutSlotsTracksNoAccounts(t *testing.T) {
	m := newManager("test-client-id", 3, nil, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	if got := len(m.Accounts()); got != 0 {
		t.Errorf("Accounts() = %d entries, want 0 without any IPC slot", got)
	}
	if m.Primary() != nil {
		t.Error("Primary() reported an account without any IPC slot")
	}
}

// The primary account is the lowest tracked pipe slot even when a higher slot
// answered the handshake first, so /api/presence and /api/state keep a stable
// meaning.
func TestManagerOrdersAccountsBySlot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "discord-ipc-0")
	second := newMockDiscordNamed(t, "discord-ipc-1")

	m := newManager("test-client-id", 2, []string{missing, second.path}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("Accounts() = %d entries, want 2", len(accounts))
	}
	if accounts[0].Name != "discord-ipc-0" {
		t.Errorf("accounts[0] name = %q, want %q (lowest slot is primary)", accounts[0].Name, "discord-ipc-0")
	}
	if accounts[1].Name != "discord-ipc-1" {
		t.Errorf("accounts[1] name = %q, want %q", accounts[1].Name, "discord-ipc-1")
	}
	if got := accounts[1].Client.State(); got != StateConnected {
		t.Errorf("accounts[1] state = %v, want connected on the slot that answered", got)
	}
	if m.Primary() != accounts[0] {
		t.Error("Primary() is not the lowest slot")
	}
}

func TestAccountSlot(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{name: "discord-ipc-0", want: 0},
		{name: "discord-ipc-9", want: 9},
		{name: "discord-ipc", want: -1},
		{name: "discord-ipc-x", want: -1},
		{name: "", want: -1},
	}

	for _, tt := range tests {
		if got := accountSlot(tt.name); got != tt.want {
			t.Errorf("accountSlot(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func waitForState(t *testing.T, client *Client, want State, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if client.State() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state = %v, want %v", client.State(), want)
}

func TestManagerAccountsReturnsCopy(t *testing.T) {
	first := newMockDiscordNamed(t, "discord-ipc-0")

	m := newManager("test-client-id", 1, []string{first.path}, zap.NewNop())
	m.Connect()
	t.Cleanup(m.Close)

	accounts := m.Accounts()
	accounts[0] = nil
	if m.Accounts()[0] == nil {
		t.Error("Accounts() exposes the internal slice")
	}
}

func TestManagerCloseClosesEveryAccount(t *testing.T) {
	first := newMockDiscordNamed(t, "discord-ipc-0")
	second := newMockDiscordNamed(t, "discord-ipc-1")

	m := newManager("test-client-id", 2, []string{first.path, second.path}, zap.NewNop())
	m.Connect()

	accounts := m.Accounts()
	m.Close()

	for i, account := range accounts {
		if got := account.Client.State(); got != StateDisconnected {
			t.Errorf("account[%d] state after Close = %v, want disconnected", i, got)
		}
	}
}

func TestAccountName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/run/user/1000/discord-ipc-3", want: "discord-ipc-3"},
		{path: `\\.\pipe\discord-ipc-7`, want: "discord-ipc-7"},
		{path: "discord-ipc-0", want: "discord-ipc-0"},
	}

	for _, tt := range tests {
		if got := accountName(tt.path); got != tt.want {
			t.Errorf("accountName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
