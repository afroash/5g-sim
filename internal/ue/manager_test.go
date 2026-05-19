package ue

import (
	"context"
	"testing"
	"time"
)

func TestManagerSpawnStop(t *testing.T) {
	mgr := NewManager(DefaultConfig(), ProfileLocal)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec, err := mgr.Spawn(ctx, SpawnOptions{SUPI: "imsi-001010000000099"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID == "" || rec.SUPI != "imsi-001010000000099" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.TunName == "" {
		t.Fatalf("expected tun name, got %+v", rec)
	}

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("list len %d", len(list))
	}

	if err := mgr.Stop(rec.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnSurvivesCancelledRequestContext(t *testing.T) {
	base := DefaultConfig()
	base.UDMAddress = "" // no UDM in unit test; focus on lifecycle ctx
	mgr := NewManager(base, ProfileLocal)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec, err := mgr.Spawn(ctx, SpawnOptions{SUPI: "imsi-001010000000077"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	got, ok := mgr.Get(rec.ID)
	if !ok {
		t.Fatal("instance missing")
	}
	if got.State == StateStopped {
		t.Fatalf("instance should not stop immediately on cancelled spawn ctx, state=%s err=%q", got.State, got.Error)
	}
	_ = mgr.Stop(rec.ID)
}

func TestManagerSpawnDefaultIdempotent(t *testing.T) {
	mgr := NewManager(DefaultConfig(), ProfileLocal)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r1, err := mgr.SpawnDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mgr.SpawnDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r1.ID != r2.ID {
		t.Fatalf("expected same default id, got %s vs %s", r1.ID, r2.ID)
	}
	_ = mgr.Stop(r1.ID)
}
