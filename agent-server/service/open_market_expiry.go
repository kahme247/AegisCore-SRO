package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	log "github.com/sirupsen/logrus"
)

const (
	openMarketExpiryBatch    = 100
	OpenMarketExpiryInterval = time.Minute
)

type openMarketExpiryRepository interface {
	ExpireDue(context.Context, time.Time, int) ([]model.MarketExpiryReturn, error)
}

type OpenMarketExpiryService struct {
	repo      openMarketExpiryRepository
	inventory model.InventoryRepository
	world     *WorldService
	interval  time.Duration
	now       func() time.Time
	done      chan struct{}
	startOnce sync.Once
}

func NewOpenMarketExpiryService(repo openMarketExpiryRepository, inventory model.InventoryRepository, world *WorldService) (*OpenMarketExpiryService, error) {
	if repo == nil || inventory == nil || world == nil {
		return nil, model.ErrMarketUnavailable
	}
	return &OpenMarketExpiryService{repo: repo, inventory: inventory, world: world, interval: OpenMarketExpiryInterval, now: time.Now, done: make(chan struct{})}, nil
}

func (s *OpenMarketExpiryService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.run(ctx)
	})
}

func (s *OpenMarketExpiryService) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.ExpireNow(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.WithError(err).Error("expire open-market listings")
			}
		}
	}
}

func (s *OpenMarketExpiryService) ExpireNow(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, model.ErrMarketUnavailable
	}
	total := 0
	for {
		returns, err := s.repo.ExpireDue(ctx, s.now().UTC(), openMarketExpiryBatch)
		if err != nil {
			return total, err
		}
		total += len(returns)
		s.publish(ctx, returns)
		if len(returns) < openMarketExpiryBatch {
			return total, nil
		}
	}
}

func (s *OpenMarketExpiryService) publish(ctx context.Context, returns []model.MarketExpiryReturn) {
	bySeller := make(map[int][]model.MarketExpiryReturn)
	for _, returned := range returns {
		bySeller[returned.Listing.SellerCharacterID] = append(bySeller[returned.Listing.SellerCharacterID], returned)
	}
	for seller, listings := range bySeller {
		player, err := s.world.GetPlayerByCharacterID(seller)
		if err != nil || player == nil {
			continue
		}
		if inventory, loadErr := s.inventory.GetCharacterInventory(ctx, int64(seller)); loadErr == nil {
			player.CommitInventory(inventory)
		}
		if player.Session == nil {
			continue
		}
		packet := network.EmptyPacket()
		packet.MessageID = opcode.OpenMarketUpdate
		packet.WriteByte(byte(len(listings)))
		for _, returned := range listings {
			packet.WriteUInt32(uint32(returned.Listing.ID))
			packet.WriteUInt32(uint32(returned.Listing.ReferenceItemID))
			packet.WriteByte(2)
			packet.WriteUInt32(uint32(returned.Listing.ExpiresAt.Unix()))
		}
		if packet.WriteError() == nil {
			_ = player.Session.Send(packet)
		}
	}
}

func (s *OpenMarketExpiryService) Wait(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
