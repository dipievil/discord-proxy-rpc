package state

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

func startRegistry(t *testing.T, interval time.Duration) *Registry {
	t.Helper()

	registry := NewRegistry(interval, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	registry.Start(ctx)
	t.Cleanup(func() {
		registry.Stop()
		cancel()
	})
	return registry
}

func TestRegistryReturnsOnePresencePerAccount(t *testing.T) {
	registry := startRegistry(t, 10*time.Millisecond)

	first := registry.For("discord-ipc-0")
	if first != registry.For("discord-ipc-0") {
		t.Error("For(same account) returned different presences")
	}
	if first == registry.For("discord-ipc-1") {
		t.Error("For(different account) returned the same presence")
	}
}

func TestRegistrySnapshotUnknownAccount(t *testing.T) {
	registry := startRegistry(t, 0)

	if _, ok := registry.Snapshot("discord-ipc-9"); ok {
		t.Error("Snapshot(unknown account) reported a snapshot")
	}

	registry.For("discord-ipc-0")
	snapshot, ok := registry.Snapshot("discord-ipc-0")
	if !ok {
		t.Fatal("Snapshot(known account) reported no snapshot")
	}
	if snapshot.Source != types.SourceNone {
		t.Errorf("Snapshot source = %q, want %q before any update", snapshot.Source, types.SourceNone)
	}
	if snapshot.Activity != nil {
		t.Errorf("Snapshot activity = %+v, want nil before any update", snapshot.Activity)
	}
}

func TestRegistrySnapshotAfterUpdate(t *testing.T) {
	registry := startRegistry(t, 10*time.Millisecond)
	presence := registry.For("discord-ipc-0")

	presence.Update(types.Activity{Details: "Account Zero"})

	snapshot, ok := registry.Snapshot("discord-ipc-0")
	if !ok {
		t.Fatal("Snapshot(known account) reported no snapshot")
	}
	activity := snapshot.Activity
	if activity == nil || activity.Details != "Account Zero" {
		t.Errorf("Snapshot activity = %+v, want details %q", activity, "Account Zero")
	}
}

func TestRegistryIsolatesAccountCoalescing(t *testing.T) {
	registry := startRegistry(t, 20*time.Millisecond)

	primary := registry.For("discord-ipc-0")
	secondary := registry.For("discord-ipc-1")

	primaryUpdates := make(chan PresenceUpdate, 4)
	secondaryUpdates := make(chan PresenceUpdate, 4)
	primary.Subscribe(func(update PresenceUpdate) { primaryUpdates <- update })
	secondary.Subscribe(func(update PresenceUpdate) { secondaryUpdates <- update })

	primary.Update(types.Activity{Details: "Primary"})
	secondary.Update(types.Activity{Details: "Secondary"})

	first := receiveUpdate(t, primaryUpdates)
	if first.Activity.Details != "Primary" {
		t.Errorf("primary account received %q, want %q", first.Activity.Details, "Primary")
	}
	second := receiveUpdate(t, secondaryUpdates)
	if second.Activity.Details != "Secondary" {
		t.Errorf("secondary account received %q, want %q", second.Activity.Details, "Secondary")
	}

	// A change on one account must not flush the other.
	primary.Update(types.Activity{Details: "Primary 2"})
	if got := receiveUpdate(t, primaryUpdates); got.Activity.Details != "Primary 2" {
		t.Errorf("primary account received %q, want %q", got.Activity.Details, "Primary 2")
	}
	select {
	case got := <-secondaryUpdates:
		t.Errorf("secondary account received an unrelated update %q", got.Activity.Details)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRegistryDiffIsPerAccount(t *testing.T) {
	registry := startRegistry(t, 10*time.Millisecond)

	primary := registry.For("discord-ipc-0")
	secondary := registry.For("discord-ipc-1")

	activity := types.Activity{Details: "Same Game"}
	primaryUpdates := make(chan PresenceUpdate, 4)
	secondaryUpdates := make(chan PresenceUpdate, 4)
	primary.Subscribe(func(update PresenceUpdate) { primaryUpdates <- update })
	secondary.Subscribe(func(update PresenceUpdate) { secondaryUpdates <- update })

	primary.Update(activity)
	receiveUpdate(t, primaryUpdates)

	// The secondary account has never announced this activity, so its own diff
	// state must treat it as new instead of suppressing it.
	secondary.Update(activity)
	receiveUpdate(t, secondaryUpdates)
}

func TestRegistryAccountsSorted(t *testing.T) {
	registry := startRegistry(t, 0)

	registry.For("discord-ipc-2")
	registry.For("discord-ipc-0")
	registry.For("discord-ipc-1")

	got := registry.Accounts()
	want := []string{"discord-ipc-0", "discord-ipc-1", "discord-ipc-2"}
	if len(got) != len(want) {
		t.Fatalf("Accounts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Accounts() = %v, want %v", got, want)
		}
	}
}

func TestRegistryForAfterStartRunsImmediately(t *testing.T) {
	registry := startRegistry(t, 10*time.Millisecond)
	presence := registry.For("discord-ipc-0")

	updates := make(chan PresenceUpdate, 1)
	presence.Subscribe(func(update PresenceUpdate) { updates <- update })
	presence.Update(types.Activity{Details: "Late Account"})

	if got := receiveUpdate(t, updates); got.Activity.Details != "Late Account" {
		t.Errorf("update details = %q, want %q", got.Activity.Details, "Late Account")
	}
}

func TestRegistryStopStopsUpdates(t *testing.T) {
	registry := NewRegistry(10*time.Millisecond, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry.Start(ctx)
	presence := registry.For("discord-ipc-0")

	updates := make(chan PresenceUpdate, 1)
	presence.Subscribe(func(update PresenceUpdate) { updates <- update })

	registry.Stop()

	presence.Update(types.Activity{Details: "After Stop"})
	select {
	case got := <-updates:
		t.Errorf("presence delivered %q after Registry.Stop", got.Activity.Details)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRegistryStopIsIdempotent(t *testing.T) {
	registry := NewRegistry(10*time.Millisecond, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry.Start(ctx)
	registry.For("discord-ipc-0")

	registry.Stop()
	registry.Stop()
}

func receiveUpdate(t *testing.T, updates <-chan PresenceUpdate) PresenceUpdate {
	t.Helper()

	select {
	case update := <-updates:
		return update
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a presence update")
		return PresenceUpdate{}
	}
}
