package exchange

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

const (
	exchangeAddItem    byte = 4
	exchangeRemoveItem byte = 5
	exchangeSetGold    byte = 13
	maxExchangeItems        = 12
)

var (
	ErrExchangeUnavailable = errors.New("player exchange is unavailable")
	ErrExchangeInvalid     = errors.New("invalid player exchange request")
	ErrExchangeState       = errors.New("invalid player exchange state")
)

type PlayerLookup interface {
	GetPlayerByUniqueId(uint32) (*model.Player, error)
}

type participant struct {
	player    *model.Player
	items     map[byte]model.ExchangeOfferedItem
	gold      uint64
	confirmed bool
	approved  bool
}

type exchangeSession struct {
	a, b       *participant
	revision   uint64
	committing bool
	terminal   bool
}

func (s *exchangeSession) side(characterID int) (*participant, *participant) {
	if s != nil && s.a != nil && s.a.player != nil && s.a.player.ID == characterID {
		return s.a, s.b
	}
	if s != nil && s.b != nil && s.b.player != nil && s.b.player.ID == characterID {
		return s.b, s.a
	}
	return nil, nil
}

type Manager struct {
	mu         sync.Mutex
	players    PlayerLookup
	repository model.ExchangeRepository
	byChar     map[int]*exchangeSession
	send       func(*server.Session, network.Packet) error
}

func NewManager(players PlayerLookup, repository model.ExchangeRepository) *Manager {
	return &Manager{players: players, repository: repository, byChar: make(map[int]*exchangeSession)}
}

func RegisterHandlers(target *server.Server, manager *Manager) error {
	if target == nil || manager == nil || manager.players == nil || manager.repository == nil {
		return ErrExchangeUnavailable
	}
	target.RegisterHandler(opcode.ExchangeStartRequest, manager.handleStart)
	target.RegisterHandler(opcode.ExchangeConfirmRequest, manager.handleConfirm)
	target.RegisterHandler(opcode.ExchangeApproveRequest, manager.handleApprove)
	target.RegisterHandler(opcode.ExchangeCancelRequest, manager.handleCancel)
	return nil
}

func (m *Manager) Busy(characterID int) bool {
	if m == nil || characterID <= 0 {
		return false
	}
	m.mu.Lock()
	s := m.byChar[characterID]
	busy := s != nil && !s.terminal
	m.mu.Unlock()
	return busy
}

// CancelPlayer releases a non-committing exchange when either participant
// disconnects. A committing exchange is left to its transaction outcome; the
// remaining peer will receive the terminal result from that path.
func (m *Manager) CancelPlayer(characterID int) {
	if m == nil || characterID <= 0 {
		return
	}
	m.mu.Lock()
	state := m.byChar[characterID]
	owner, peer := state.side(characterID)
	if state == nil || owner == nil || peer == nil || state.terminal || state.committing {
		m.mu.Unlock()
		return
	}
	state.terminal = true
	delete(m.byChar, state.a.player.ID)
	delete(m.byChar, state.b.player.ID)
	peerSession := peer.player.Session
	m.mu.Unlock()
	_ = m.sendPacket(peerSession, exchangeCancelled())
}

func (m *Manager) handleStart(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 4 {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	targetUID, err := packet.ReadUInt32()
	if err != nil || packet.Remaining() != 0 {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	requester, err := m.resolve(session)
	if err != nil || targetUID == requester.GetUniqueID() {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	target, err := m.players.GetPlayerByUniqueId(targetUID)
	if err != nil || target == nil || target.Session == nil || target.ID <= 0 || target.GetLifeState() != model.Alive {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	known := requester.GetKnownObjectList()
	if known == nil || known.GetKnownObjects()[targetUID] != target {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	first, second := orderedPlayers(requester, target)
	first.LockEconomy()
	second.LockEconomy()
	defer second.UnlockEconomy()
	defer first.UnlockEconomy()

	m.mu.Lock()
	if existing := m.byChar[requester.ID]; existing != nil {
		_, peer := existing.side(requester.ID)
		if peer != nil && peer.player == target && !existing.terminal {
			m.mu.Unlock()
			return m.sendPacket(session, exchangeStartSuccess(targetUID))
		}
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	if m.byChar[target.ID] != nil {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	state := &exchangeSession{
		a: &participant{player: requester, items: make(map[byte]model.ExchangeOfferedItem)},
		b: &participant{player: target, items: make(map[byte]model.ExchangeOfferedItem)},
	}
	m.byChar[requester.ID], m.byChar[target.ID] = state, state
	m.mu.Unlock()
	if err = m.sendPacket(session, exchangeStartSuccess(targetUID)); err != nil {
		m.cancelState(state)
		return err
	}
	started := network.EmptyPacket()
	started.MessageID = opcode.ExchangeStartedResponse
	started.WriteUInt32(requester.GetUniqueID())
	if err = m.sendPacket(target.Session, started); err != nil {
		m.cancelState(state)
		return err
	}
	return nil
}

func (m *Manager) HandleInventoryOperation(_ context.Context, session *server.Session, player *model.Player, operation byte, packet *network.Packet) error {
	if m == nil || player == nil || player.Session != session || packet == nil {
		return m.sendPacket(session, inventoryOperationFailure())
	}
	var source byte
	var gold uint64
	switch operation {
	case exchangeAddItem, exchangeRemoveItem:
		if packet.Remaining() != 1 {
			return m.sendPacket(session, inventoryOperationFailure())
		}
		source, _ = packet.ReadByte()
	case exchangeSetGold:
		if packet.Remaining() != 8 {
			return m.sendPacket(session, inventoryOperationFailure())
		}
		gold, _ = packet.ReadUInt64()
	default:
		return m.sendPacket(session, inventoryOperationFailure())
	}
	if packet.Remaining() != 0 {
		return m.sendPacket(session, inventoryOperationFailure())
	}

	player.LockEconomy()
	defer player.UnlockEconomy()
	m.mu.Lock()
	state := m.byChar[player.ID]
	owner, peer := state.side(player.ID)
	if state == nil || owner == nil || peer == nil || state.terminal || state.committing {
		m.mu.Unlock()
		return m.sendPacket(session, inventoryOperationFailure())
	}
	changedItems := false
	switch operation {
	case exchangeAddItem:
		if source < 13 || len(owner.items) >= maxExchangeItems {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		inventory := player.Inventory.Snapshot()
		item, ok := inventory.Items[source]
		ref, refOK := model.RefItems[item.GetRefObjectID()]
		if !ok || !refOK || !ref.IsTradable {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		for _, offered := range owner.items {
			if offered.SourceSlot == source || offered.ItemID == item.ID {
				m.mu.Unlock()
				return m.sendPacket(session, inventoryOperationFailure())
			}
		}
		exchangeSlot := firstFreeExchangeSlot(owner.items)
		owner.items[exchangeSlot] = model.ExchangeOfferedItem{SourceSlot: source, ExchangeSlot: exchangeSlot, ItemID: item.ID}
		changedItems = true
	case exchangeRemoveItem:
		if _, ok := owner.items[source]; !ok {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		delete(owner.items, source)
		changedItems = true
	case exchangeSetGold:
		player.Mutex.Lock()
		available := player.Gold
		player.Mutex.Unlock()
		if gold > available {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		owner.gold = gold
	}
	state.revision++
	state.a.confirmed, state.a.approved = false, false
	state.b.confirmed, state.b.approved = false, false
	ownerCopy, peerCopy := copyParticipant(owner), copyParticipant(peer)
	m.mu.Unlock()

	if err := m.sendPacket(session, inventoryOperationSuccess(operation)); err != nil {
		return err
	}
	if changedItems {
		return m.sendSnapshots(ownerCopy, peerCopy)
	}
	update := network.EmptyPacket()
	update.MessageID = opcode.ExchangeUpdateResponse
	update.WriteByte(2)
	update.WriteUInt64(gold)
	return m.sendPacket(peerCopy.player.Session, update)
}

func (m *Manager) handleConfirm(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeConfirmResponse))
	}
	player, err := m.resolve(session)
	if err != nil {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeConfirmResponse))
	}
	m.mu.Lock()
	state := m.byChar[player.ID]
	owner, peer := state.side(player.ID)
	if state == nil || owner == nil || peer == nil || state.terminal || state.committing {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeConfirmResponse))
	}
	owner.confirmed = true
	peerSession := peer.player.Session
	m.mu.Unlock()
	if err = m.sendPacket(session, exchangeSuccess(opcode.ExchangeConfirmResponse)); err != nil {
		return err
	}
	notice := network.EmptyPacket()
	notice.MessageID = opcode.ExchangeConfirmedResponse
	return m.sendPacket(peerSession, notice)
}

func (m *Manager) handleApprove(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeApproveResponse))
	}
	player, err := m.resolve(session)
	if err != nil {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeApproveResponse))
	}
	m.mu.Lock()
	state := m.byChar[player.ID]
	owner, peer := state.side(player.ID)
	if state == nil || owner == nil || peer == nil || state.terminal || state.committing || !owner.confirmed || !peer.confirmed {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeApproveResponse))
	}
	owner.approved = true
	if !peer.approved {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeSuccess(opcode.ExchangeApproveResponse))
	}
	state.committing = true
	a, b := copyParticipant(state.a), copyParticipant(state.b)
	m.mu.Unlock()

	first, second := orderedPlayers(a.player, b.player)
	first.LockEconomy()
	second.LockEconomy()
	defer second.UnlockEconomy()
	defer first.UnlockEconomy()
	commitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	result, commitErr := m.repository.CommitExchange(commitCtx, participantOffer(a), participantOffer(b))
	cancel()
	if commitErr != nil {
		m.finishState(state)
		if errors.Is(commitErr, model.ErrExchangeOutcomeUnknown) {
			a.player.Session.Close()
			b.player.Session.Close()
			return commitErr
		}
		_ = m.sendPacket(a.player.Session, exchangeCancelled())
		_ = m.sendPacket(b.player.Session, exchangeCancelled())
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeApproveResponse))
	}
	a.player.CommitEconomy(result.FirstGold, result.FirstInventory)
	b.player.CommitEconomy(result.SecondGold, result.SecondInventory)
	m.finishState(state)
	if err = m.sendPacket(session, exchangeSuccess(opcode.ExchangeApproveResponse)); err != nil {
		return err
	}
	complete := network.EmptyPacket()
	complete.MessageID = opcode.ExchangeApprovedResponse
	if err = m.sendPacket(a.player.Session, complete); err != nil {
		return err
	}
	return m.sendPacket(b.player.Session, complete)
}

func (m *Manager) handleCancel(_ context.Context, session *server.Session, packet network.Packet) error {
	if packet.Remaining() != 0 {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeCancelResponse))
	}
	player, err := m.resolve(session)
	if err != nil {
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeCancelResponse))
	}
	m.mu.Lock()
	state := m.byChar[player.ID]
	owner, peer := state.side(player.ID)
	if state == nil || owner == nil || peer == nil || state.terminal || state.committing {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeCancelResponse))
	}
	state.terminal = true
	delete(m.byChar, state.a.player.ID)
	delete(m.byChar, state.b.player.ID)
	peerSession := peer.player.Session
	m.mu.Unlock()
	if err = m.sendPacket(session, exchangeSuccess(opcode.ExchangeCancelResponse)); err != nil {
		return err
	}
	return m.sendPacket(peerSession, exchangeCancelled())
}

func (m *Manager) resolve(session *server.Session) (*model.Player, error) {
	if m == nil || m.players == nil || session == nil || session.UserContext.UniqueID == 0 || session.UserContext.CharacterID <= 0 {
		return nil, ErrExchangeInvalid
	}
	player, err := m.players.GetPlayerByUniqueId(session.UserContext.UniqueID)
	if err != nil || player == nil || player.Session != session || player.ID != session.UserContext.CharacterID {
		return nil, ErrExchangeInvalid
	}
	return player, nil
}

func (m *Manager) sendSnapshots(owner, peer *participant) error {
	if owner == nil || peer == nil {
		return ErrExchangeState
	}
	if err := m.sendPacket(owner.player.Session, buildItemSnapshot(owner, true)); err != nil {
		return err
	}
	if err := m.sendPacket(peer.player.Session, buildItemSnapshot(owner, false)); err != nil {
		return err
	}
	return nil
}

func buildItemSnapshot(owner *participant, ownView bool) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ExchangeUpdateItemsResponse
	p.WriteUInt32(owner.player.GetUniqueID())
	inventory := owner.player.Inventory.Snapshot()
	items := sortedParticipantItems(owner.items)
	valid := items[:0]
	for _, offered := range items {
		item, ok := inventory.Items[offered.SourceSlot]
		if ok && item.ID == offered.ItemID {
			valid = append(valid, offered)
		}
	}
	p.WriteByte(byte(len(valid)))
	for _, offered := range valid {
		item, ok := inventory.Items[offered.SourceSlot]
		if !ok {
			continue
		}
		if ownView {
			p.WriteByte(offered.SourceSlot)
		}
		p.WriteByte(offered.ExchangeSlot)
		model.WriteRentInfo(&p, item)
		p.WriteUInt32(item.GetRefObjectID())
		model.WriteInventoryItem(&p, item)
	}
	return p
}

func (m *Manager) sendPacket(session *server.Session, packet network.Packet) error {
	if session == nil {
		return ErrExchangeInvalid
	}
	if m != nil && m.send != nil {
		return m.send(session, packet)
	}
	return session.Send(packet)
}

func (m *Manager) cancelState(state *exchangeSession) {
	if state == nil {
		return
	}
	m.finishState(state)
	_ = m.sendPacket(state.a.player.Session, exchangeCancelled())
	_ = m.sendPacket(state.b.player.Session, exchangeCancelled())
}

func (m *Manager) finishState(state *exchangeSession) {
	m.mu.Lock()
	if state != nil {
		state.terminal = true
		delete(m.byChar, state.a.player.ID)
		delete(m.byChar, state.b.player.ID)
	}
	m.mu.Unlock()
}

func participantOffer(p *participant) model.ExchangeOffer {
	return model.ExchangeOffer{CharacterID: int64(p.player.ID), UserID: p.player.Session.UserContext.UserID, Gold: p.gold, Items: sortedParticipantItems(p.items)}
}

func copyParticipant(source *participant) *participant {
	if source == nil {
		return nil
	}
	result := *source
	result.items = make(map[byte]model.ExchangeOfferedItem, len(source.items))
	for slot, item := range source.items {
		result.items[slot] = item
	}
	return &result
}

func sortedParticipantItems(items map[byte]model.ExchangeOfferedItem) []model.ExchangeOfferedItem {
	result := make([]model.ExchangeOfferedItem, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ExchangeSlot < result[j].ExchangeSlot })
	return result
}

func firstFreeExchangeSlot(items map[byte]model.ExchangeOfferedItem) byte {
	for slot := byte(0); slot < maxExchangeItems; slot++ {
		if _, used := items[slot]; !used {
			return slot
		}
	}
	return 0xff
}

func orderedPlayers(a, b *model.Player) (*model.Player, *model.Player) {
	if a.ID <= b.ID {
		return a, b
	}
	return b, a
}

func exchangeStartSuccess(target uint32) network.Packet {
	p := exchangeSuccess(opcode.ExchangeStartResponse)
	p.WriteUInt32(target)
	return p
}

func exchangeSuccess(messageID uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	p.WriteByte(1)
	return p
}

func exchangeFailure(messageID uint16) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = messageID
	p.WriteByte(2)
	p.WriteUInt16(0)
	return p
}

func exchangeCancelled() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ExchangeCancelledResponse
	return p
}

func inventoryOperationSuccess(operation byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(1)
	p.WriteByte(operation)
	return p
}

func inventoryOperationFailure() network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(2)
	p.WriteByte(0)
	return p
}

func (m *Manager) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("exchange manager (%d active participants)", len(m.byChar))
}
