package ipc

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/platform"
)

// Account is one Discord IPC account: a stable identity (the pipe slot, e.g.
// "discord-ipc-1") and the single-socket Client that owns that slot. The
// identity comes from the socket name, never from the Discord user, so it is
// stable across restarts and carries no secrets.
type Account struct {
	Client *Client

	// claimed is the pipe slot bound at Connect time. It stays the account's
	// stable key and the reported identity until the client opens a session on
	// another slot.
	claimed string
}

// Name reports where the account is now: the pipe slot of its most recent
// session, falling back to the claimed slot before the first successful
// connection. A single-account client scans every slot, so the name follows it
// to the slot it actually reconnected to instead of freezing at the first
// connect.
func (a *Account) Name() string {
	if socket := accountName(a.Client.Socket()); socket != "" {
		return socket
	}
	return a.claimed
}

// Key is the account's stable identity: the slot claimed at Connect time. It
// survives reconnects that move a single-account client to another slot, so
// presence state stays keyed to one account while Name reports the live slot.
func (a *Account) Key() string {
	return a.claimed
}

// Manager owns one Client per tracked Discord IPC account. Each client stays
// single-socket; the manager only decides which pipe slot belongs to which
// account and keeps the accounts ordered by slot, so callers can resolve the
// primary through Primary.
type Manager struct {
	clientID    string
	maxAccounts int
	paths       []string
	logger      *zap.Logger

	mu       sync.RWMutex
	accounts []*Account
}

// NewManager returns a Manager that tracks up to maxAccounts Discord IPC
// accounts on the platform's discord-ipc-{0..9} sockets. maxAccounts is
// clamped to that slot list, so a platform without slots tracks no accounts.
func NewManager(clientID string, maxAccounts int, logger *zap.Logger) *Manager {
	return newManager(clientID, maxAccounts, platform.IPCPaths(), logger)
}

func newManager(clientID string, maxAccounts int, paths []string, logger *zap.Logger) *Manager {
	if maxAccounts < 1 {
		maxAccounts = 1
	}
	if maxAccounts > len(paths) {
		maxAccounts = len(paths)
	}
	return &Manager{
		clientID:    clientID,
		maxAccounts: maxAccounts,
		paths:       paths,
		logger:      logger,
	}
}

// Connect claims a pipe slot for every account and opens the initial session.
// With a single account it scans every slot in order and uses the first one
// that handshakes, exactly like a bare Client; with several accounts it gives
// each one the lowest-numbered slot not claimed by another account. A slot
// that cannot be opened yet is kept for that account, so the normal reconnect
// loop attaches to it once Discord appears.
func (m *Manager) Connect() {
	accounts := make([]*Account, 0, m.maxAccounts)

	if m.maxAccounts == 1 {
		client := NewWithPaths(m.clientID, m.paths, m.logger)
		if err := client.Connect(); err != nil {
			m.logger.Warn("initial IPC connect failed, will reconnect", zap.Error(err))
		}
		accounts = append(accounts, &Account{
			claimed: accountName(firstNonEmpty(client.Socket(), m.paths[0])),
			Client:  client,
		})
	} else {
		claimed := make(map[string]bool, m.maxAccounts)
		for range m.maxAccounts {
			client, path := m.connectSlot(claimed)
			claimed[path] = true
			accounts = append(accounts, &Account{claimed: accountName(path), Client: client})
		}
	}

	// Accounts are kept in pipe-slot order so the connected primary — the
	// lowest slot that is up, else the lowest slot — can be resolved from the
	// list without depending on which slot answered first.
	sort.SliceStable(accounts, func(i, j int) bool {
		return accountSlot(accounts[i].Key()) < accountSlot(accounts[j].Key())
	})

	m.mu.Lock()
	m.accounts = accounts
	m.mu.Unlock()

	for _, account := range accounts {
		m.logger.Info("IPC account ready",
			zap.String("account", account.Name()),
			zap.String("state", account.Client.State().String()))
	}
}

// connectSlot binds the next account to the lowest-numbered unclaimed slot that
// answers the handshake, falling back to the lowest unclaimed slot so a later
// Discord start still has a stable identity to reconnect to.
func (m *Manager) connectSlot(claimed map[string]bool) (*Client, string) {
	fallback := ""
	for _, path := range m.paths {
		if claimed[path] {
			continue
		}
		if fallback == "" {
			fallback = path
		}

		client := NewWithPaths(m.clientID, []string{path}, m.logger)
		if err := client.Connect(); err != nil {
			continue
		}
		return client, path
	}

	client := NewWithPaths(m.clientID, []string{fallback}, m.logger)
	m.logger.Warn("no free Discord IPC pipe for account yet, will keep retrying",
		zap.String("account", accountName(fallback)))
	return client, fallback
}

// Accounts returns the tracked accounts ordered by pipe slot. The returned
// slice is a copy; the set of accounts is fixed after Connect, while each
// account's Name follows its client across reconnects.
func (m *Manager) Accounts() []*Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Account, len(m.accounts))
	copy(out, m.accounts)
	return out
}

// Primary returns the account the proxy reports as its own: the lowest tracked
// slot that is currently connected, falling back to the lowest tracked slot
// while none is. It is resolved on every call, so a dead low slot never masks a
// live higher one and the primary follows reconnects. It returns nil before
// Connect.
func (m *Manager) Primary() *Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.accounts) == 0 {
		return nil
	}
	for _, account := range m.accounts {
		if account.Client.State() == StateConnected {
			return account
		}
	}
	return m.accounts[0]
}

// Account looks an account up by its identity: the slot it is connected to now
// or the slot it claimed at Connect time, so a selector keeps working across a
// reconnect that moved the account.
func (m *Manager) Account(name string) (*Account, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, account := range m.accounts {
		if account.Name() == name || account.Key() == name {
			return account, true
		}
	}
	return nil, false
}

// Close terminates every account's IPC session. The reconnect loops are
// stopped by their context, which the caller owns.
func (m *Manager) Close() {
	for _, account := range m.Accounts() {
		_ = account.Client.Close()
	}
}

func accountName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// accountSlot returns the numeric pipe slot of an account identity, so accounts
// order as discord-ipc-0, discord-ipc-1, … and not lexically. An identity that
// carries no slot sorts first.
func accountSlot(name string) int {
	i := strings.LastIndexByte(name, '-')
	if i < 0 || i+1 == len(name) {
		return -1
	}
	slot, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return -1
	}
	return slot
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
