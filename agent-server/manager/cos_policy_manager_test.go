package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type cosPolicyRepositoryFake struct {
	mu     sync.Mutex
	policy model.COSPolicySnapshot
	err    error
}

func (f *cosPolicyRepositoryFake) COSPolicy(context.Context) (model.COSPolicySnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.policy, f.err
}

func TestCOSPolicyManagerRefreshesAtomicallyAndRetainsLastGoodPolicy(t *testing.T) {
	policy := model.DefaultCOSPolicySnapshot()
	policy.Kinds[model.COSRideTransport].InheritOwnerSpeed = true
	policy.Settings.PickupCollectionRange = 220
	repository := &cosPolicyRepositoryFake{policy: policy}
	manager, err := NewCOSPolicyManager(repository, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Current().ForKind(model.COSRideTransport).InheritOwnerSpeed {
		t.Fatal("safe defaults made ride transports inherit owner speed")
	}
	if err = manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current := manager.Current(); !current.ForKind(model.COSRideTransport).InheritOwnerSpeed || current.Settings.PickupCollectionRange != 220 {
		t.Fatalf("refreshed policy = %#v", current)
	}
	repository.mu.Lock()
	repository.err = errors.New("database unavailable")
	repository.policy = model.COSPolicySnapshot{}
	repository.mu.Unlock()
	if err = manager.Refresh(context.Background()); err == nil {
		t.Fatal("failed refresh returned nil")
	}
	if current := manager.Current(); !current.ForKind(model.COSRideTransport).InheritOwnerSpeed || current.Settings.PickupCollectionRange != 220 {
		t.Fatalf("policy after failed refresh = %#v", current)
	}
}

func TestCOSPolicyManagerStopsWithContext(t *testing.T) {
	repository := &cosPolicyRepositoryFake{policy: model.DefaultCOSPolicySnapshot()}
	manager, err := NewCOSPolicyManager(repository, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err = manager.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
}
