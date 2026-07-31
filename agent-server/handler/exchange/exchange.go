package exchange

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	exchangeAddItem    byte = 4
	exchangeRemoveItem byte = 5
	exchangeSetGold    byte = 13
	maxExchangeItems        = 12
	exchangeCooldown        = 10 * time.Second
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
	started    bool
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
	cooldowns  map[exchangePair]time.Time
	send       func(*server.Session, network.Packet) error
}

type exchangePair struct{ first, second int }

func NewManager(players PlayerLookup, repository model.ExchangeRepository) *Manager {
	return &Manager{players: players, repository: repository, byChar: make(map[int]*exchangeSession), cooldowns: make(map[exchangePair]time.Time)}
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
	first, second := orderedPlayers(requester, target)
	first.LockEconomy()
	second.LockEconomy()
	defer second.UnlockEconomy()
	defer first.UnlockEconomy()

	m.mu.Lock()
	if m.byChar[requester.ID] != nil || m.byChar[target.ID] != nil {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	if known == nil || known.GetKnownObjects()[targetUID] != target {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	pair := orderedPair(requester.ID, target.ID)
	now := time.Now()
	if until := m.cooldowns[pair]; until.After(now) {
		m.mu.Unlock()
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeStartResponse))
	}
	delete(m.cooldowns, pair)
	state := &exchangeSession{
		a: &participant{player: requester, items: make(map[byte]model.ExchangeOfferedItem)},
		b: &participant{player: target, items: make(map[byte]model.ExchangeOfferedItem)},
	}
	m.byChar[requester.ID], m.byChar[target.ID] = state, state
	m.mu.Unlock()
	if err = m.sendPacket(target.Session, exchangeInvitation(requester.GetUniqueID())); err != nil {
		m.cancelState(state)
		return err
	}
	return nil
}

// AnswerInvitation processes the client's 0x3080 exchange invitation answer.
// The shared invitation envelope sends only its kind and answer, so the pending
// exchange state identifies the requester and recipient.
func (m *Manager) AnswerInvitation(_ context.Context, session *server.Session, accepted bool) (bool, error) {
	player, err := m.resolve(session)
	if err != nil {
		return false, nil
	}
	m.mu.Lock()
	state := m.byChar[player.ID]
	owner, peer := state.side(player.ID)
	if state == nil || owner == nil || owner != state.b || peer == nil || state.started || state.terminal || state.committing {
		m.mu.Unlock()
		return false, nil
	}
	if !accepted {
		state.terminal = true
		delete(m.byChar, state.a.player.ID)
		delete(m.byChar, state.b.player.ID)
		peerSession := peer.player.Session
		m.mu.Unlock()
		return true, m.sendPacket(peerSession, exchangeCancelled())
	}
	state.started = true
	peerSession := peer.player.Session
	peerUID := peer.player.GetUniqueID()
	m.mu.Unlock()
	if err = m.sendPacket(session, exchangeStartSuccess(peerUID)); err != nil {
		m.cancelState(state)
		return true, err
	}
	if err = m.sendPacket(peerSession, exchangeStarted(player.GetUniqueID())); err != nil {
		m.cancelState(state)
		return true, err
	}
	return true, nil
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
	if state == nil || owner == nil || peer == nil || !state.started || state.terminal || state.committing {
		m.mu.Unlock()
		return m.sendPacket(session, inventoryOperationFailure())
	}
	changedItems := false
	var exchangeSlot byte
	switch operation {
	case exchangeAddItem:
		if source < 13 || len(owner.items) >= maxExchangeItems {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		inventory := player.Inventory.Snapshot()
		item, ok := inventory.Items[source]
		ref, refOK := model.RefItems[item.GetRefObjectID()]
		if !ok || !refOK || !ref.IsTradable || hasActiveItemService(player, source, time.Now().UTC()) {
			m.mu.Unlock()
			return m.sendPacket(session, inventoryOperationFailure())
		}
		for _, offered := range owner.items {
			if offered.SourceSlot == source || offered.ItemID == item.ID {
				m.mu.Unlock()
				return m.sendPacket(session, inventoryOperationFailure())
			}
		}
		exchangeSlot = firstFreeExchangeSlot(owner.items)
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

	var response network.Packet
	switch operation {
	case exchangeAddItem:
		response = inventoryOperationAddSuccess(source, exchangeSlot)
	case exchangeRemoveItem:
		response = inventoryOperationRemoveSuccess(source)
	case exchangeSetGold:
		response = inventoryOperationGoldSuccess(gold)
	}
	if err := m.sendPacket(session, response); err != nil {
		return err
	}
	if changedItems {
		return m.sendSnapshots(ownerCopy, peerCopy)
	}
	return m.sendPacket(peerCopy.player.Session, buildExchangeGoldUpdate(gold))
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
	if state == nil || owner == nil || peer == nil || !state.started || state.terminal || state.committing {
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
	if !validateParticipantOffer(a, time.Now().UTC()) || !validateParticipantOffer(b, time.Now().UTC()) {
		m.finishState(state)
		_ = m.sendPacket(a.player.Session, exchangeCancelled())
		_ = m.sendPacket(b.player.Session, exchangeCancelled())
		return m.sendPacket(session, exchangeFailure(opcode.ExchangeApproveResponse))
	}
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
	m.finishCompletedState(state)
	if err = m.sendPacket(session, exchangeSuccess(opcode.ExchangeApproveResponse)); err != nil {
		return err
	}
	complete := network.EmptyPacket()
	complete.MessageID = opcode.ExchangeApprovedResponse
	if err = m.sendPacket(a.player.Session, complete); err != nil {
		return err
	}
	if err = m.sendPacket(b.player.Session, complete); err != nil {
		return err
	}
	if err = m.sendPermanentReceivedItemUpdates(a.player, result.FirstReceivedSlots); err != nil {
		return err
	}
	if err = m.sendPermanentReceivedItemUpdates(b.player, result.SecondReceivedSlots); err != nil {
		return err
	}
	if err = m.sendPacket(a.player.Session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: result.FirstGold})); err != nil {
		return err
	}
	return m.sendPacket(b.player.Session, vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: result.SecondGold}))
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
		item = projectTemporaryEnhancement(owner.player, offered.SourceSlot, item, time.Now().UTC())
		model.WriteRentInfo(&p, item)
		p.WriteUInt32(item.GetRefObjectID())
		model.WriteInventoryItem(&p, item)
	}
	return p
}

func projectTemporaryEnhancement(player *model.Player, slot byte, item model.Item, now time.Time) model.Item {
	if player == nil {
		return item
	}
	for _, state := range player.ItemServiceStates(now) {
		effect := state.Effect
		if !model.ItemServiceAffectsStats(effect) || !effect.TargetSlotSet || effect.TargetSlot != slot || item.OptLevel != effect.TargetBaseOptLevel {
			continue
		}
		item.OptLevel = effect.TargetProjectedOptLevel
		item.RentInfo = model.RentInfo{
			Type:            1,
			CanDelete:       0,
			PeriodBeginTime: uint32(effect.ActivatedAt.Unix()),
			PeriodEndTime:   uint32(effect.ExpiresAt.Unix()),
		}
		return item
	}
	return item
}

func hasActiveItemService(player *model.Player, slot byte, now time.Time) bool {
	if player == nil {
		return false
	}
	for _, state := range player.ItemServiceStates(now) {
		if state.Effect.TargetSlotSet && state.Effect.TargetSlot == slot {
			return true
		}
	}
	return false
}

func validateParticipantOffer(participant *participant, now time.Time) bool {
	if participant == nil || participant.player == nil {
		return false
	}
	inventory := participant.player.Inventory.Snapshot()
	for _, offered := range participant.items {
		item, exists := inventory.Items[offered.SourceSlot]
		ref, refExists := model.RefItems[item.GetRefObjectID()]
		if !exists || item.ID != offered.ItemID || !refExists || !ref.IsTradable || hasActiveItemService(participant.player, offered.SourceSlot, now) {
			return false
		}
	}
	return true
}

func (m *Manager) sendPermanentReceivedItemUpdates(player *model.Player, slots []byte) error {
	if player == nil || player.Session == nil || len(slots) == 0 {
		return nil
	}
	inventory := player.Inventory.Snapshot()
	expiration := uint32(0)
	for _, slot := range slots {
		item, exists := inventory.Items[slot]
		if !exists || !item.IsEquipment() {
			continue
		}
		level := item.OptLevel
		packet, err := vsro188.BuildInventoryItemUpdate(vsro188.InventoryItemUpdate{
			ItemKind:  slot,
			Flags:     0x82,
			Flag02U8:  &level,
			Flag80U32: &expiration,
		})
		if err != nil {
			return err
		}
		if err = m.sendPacket(player.Session, packet); err != nil {
			return err
		}
	}
	return nil
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

func (m *Manager) finishCompletedState(state *exchangeSession) {
	m.mu.Lock()
	if state != nil {
		state.terminal = true
		delete(m.byChar, state.a.player.ID)
		delete(m.byChar, state.b.player.ID)
		m.cooldowns[orderedPair(state.a.player.ID, state.b.player.ID)] = time.Now().Add(exchangeCooldown)
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

func orderedPair(a, b int) exchangePair {
	if a <= b {
		return exchangePair{first: a, second: b}
	}
	return exchangePair{first: b, second: a}
}

func exchangeStartSuccess(target uint32) network.Packet {
	p := exchangeSuccess(opcode.ExchangeStartResponse)
	p.WriteUInt32(target)
	return p
}

func exchangeInvitation(inviter uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ExchangeInvitationResponse
	p.WriteByte(1)
	p.WriteUInt32(inviter)
	return p
}

func exchangeStarted(peer uint32) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ExchangeStartedResponse
	p.WriteUInt32(peer)
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
	// The client handler for 0x3088 unconditionally reads this cancellation
	// reason as a uint16. Zero expresses a generic peer cancellation.
	p.WriteUInt16(0)
	return p
}

func buildExchangeGoldUpdate(gold uint64) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ExchangeUpdateResponse
	p.WriteByte(2)
	p.WriteUInt64(gold)
	return p
}

func inventoryOperationAddSuccess(source, exchangeSlot byte) network.Packet {
	p := inventoryOperationResult(1, exchangeAddItem)
	p.WriteByte(source)
	p.WriteByte(exchangeSlot)
	return p
}

func inventoryOperationRemoveSuccess(exchangeSlot byte) network.Packet {
	p := inventoryOperationResult(1, exchangeRemoveItem)
	p.WriteByte(exchangeSlot)
	return p
}

func inventoryOperationGoldSuccess(gold uint64) network.Packet {
	p := inventoryOperationResult(1, exchangeSetGold)
	p.WriteUInt64(gold)
	return p
}

func inventoryOperationFailure() network.Packet {
	return inventoryOperationResult(2, 0)
}

func inventoryOperationResult(result, operation byte) network.Packet {
	p := network.EmptyPacket()
	p.MessageID = opcode.ItemOperationResponse
	p.WriteByte(result)
	p.WriteByte(operation)
	return p
}

func (m *Manager) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("exchange manager (%d active participants)", len(m.byChar))
}
