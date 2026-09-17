package character

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type FortressSiegeHandler struct {
	repository model.FortressSiegeRepository
	world      gachaWorld
	send       func(*server.Session, network.Packet) error
	now        func() time.Time
}

func RegisterFortressSiegeHandler(target *server.Server, repository model.FortressSiegeRepository, world gachaWorld) (*FortressSiegeHandler, error) {
	if target == nil || repository == nil || world == nil {
		return nil, fmt.Errorf("configure fortress siege handler: dependencies are required")
	}
	h := &FortressSiegeHandler{repository: repository, world: world, now: time.Now}
	target.RegisterHandler(opcode.SiegeActionRequest, h.handleAction)
	return h, nil
}

func (h *FortressSiegeHandler) handleAction(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := vsro188.DecodeSiegeApplicationRequest(&packet)
	if err != nil {
		return h.failure(session, 0, 5)
	}
	if session == nil || session.UserContext.CharacterID <= 0 || session.UserContext.UniqueID == 0 {
		return h.failure(session, request.Action, 3)
	}
	player, err := resolveSelectingPlayer(h.world, session)
	if err != nil || session.UserContext.SelectedNPCUniqueID == 0 {
		return h.failure(session, request.Action, 3)
	}
	if err = validateNPCSelection(h.world, player, session.UserContext.SelectedNPCUniqueID); err != nil {
		return h.failure(session, request.Action, 3)
	}
	npc, err := h.world.GetObjectByUniqueId(session.UserContext.SelectedNPCUniqueID)
	if err != nil || npc == nil || !strings.HasSuffix(npc.GetName(), "_FORTRESS_OFFICIAL") {
		return h.failure(session, request.Action, 3)
	}
	fortressID, err := h.repository.ResolveFortressApplicationTarget(ctx)
	if err != nil {
		return h.failure(session, request.Action, siegeActionError(err))
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	var application model.FortressApplication
	switch request.Action {
	case vsro188.SiegeActionApplicationApply:
		application, err = h.repository.ApplyFortressSiege(ctx, fortressID, player.ID, model.FortressRequestType(request.RequestType), now)
	case vsro188.SiegeActionApplicationCancel:
		application, err = h.repository.CancelFortressSiege(ctx, fortressID, player.ID, now)
	default:
		return h.failure(session, request.Action, 5)
	}
	if err != nil {
		return h.failure(session, request.Action, siegeActionError(err))
	}
	update, err := vsro188.BuildSiegeApplicationUpdate(uint32(application.FortressID), byte(application.Type), request.Action == vsro188.SiegeActionApplicationCancel)
	if err != nil {
		return err
	}
	h.world.Broadcast(update)
	return nil
}

func siegeActionError(err error) uint16 {
	switch {
	case errors.Is(err, model.ErrSocialRepositoryUnavailable), errors.Is(err, model.ErrSocialNotFound):
		return 3
	case errors.Is(err, model.ErrSocialInsufficientLevel):
		return 10253
	case errors.Is(err, model.ErrSocialCapacity):
		return 10252
	case errors.Is(err, model.ErrSocialAccessDenied):
		return 10247
	case errors.Is(err, model.ErrSocialConflict):
		return 10257
	default:
		return 2
	}
}

func (h *FortressSiegeHandler) failure(session *server.Session, action byte, code uint16) error {
	if session == nil {
		return fmt.Errorf("fortress siege session is unavailable")
	}
	packet := vsro188.BuildSiegeActionFailure(action, code)
	if h.send != nil {
		return h.send(session, packet)
	}
	return session.Send(packet)
}
