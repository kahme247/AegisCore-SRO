package service

import (
	"context"
	"errors"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

var ErrDimensionHoleEntryUnavailable = errors.New("Dimension Hole entry is unavailable")

// DimensionHoleEntryCoordinator owns the loading/world-transfer boundary. The
// concrete TeleportHandler implementation is wired only once destination and
// packet ordering are proven; admission rollback remains testable meanwhile.
type DimensionHoleEntryCoordinator interface {
	SubmitDimensionHoleEntry(context.Context, *model.Player, DimensionHoleAdmission, DimensionHoleEntryDestination) error
}

// DimensionHoleEntryDestination is the minimum authored transfer target. It
// intentionally excludes normal-teleport IDs, fees, and level requirements.
type DimensionHoleEntryDestination struct {
	WorldID uint16
	Region  int16
	X       float32
	Y       float32
	Z       float32
}

// DimensionHoleEntryDestinationResolver supplies the authored position inside
// the selected direct GameWorld. Admission proves world/layer ownership, but it
// deliberately cannot invent the target region or coordinates.
type DimensionHoleEntryDestinationResolver interface {
	ResolveDimensionHoleEntry(context.Context, DimensionHoleAdmission) (DimensionHoleEntryDestination, error)
}

type DimensionHoleEntryService struct {
	admissions   *DimensionHoleAdmissionService
	destinations DimensionHoleEntryDestinationResolver
	teleport     DimensionHoleEntryCoordinator
}

func NewDimensionHoleEntryService(admissions *DimensionHoleAdmissionService, destinations DimensionHoleEntryDestinationResolver, teleport DimensionHoleEntryCoordinator) (*DimensionHoleEntryService, error) {
	if admissions == nil || destinations == nil || teleport == nil {
		return nil, ErrDimensionHoleEntryUnavailable
	}
	return &DimensionHoleEntryService{admissions: admissions, destinations: destinations, teleport: teleport}, nil
}

// Enter admits the clicking player. When the portal owner enters, every live,
// eligible party member is staged into the same direct-world scope so the
// original owner-led party crosses the gate as one unit.
func (s *DimensionHoleEntryService) Enter(ctx context.Context, portalUID uint32, player *model.Player) (DimensionHoleAdmission, error) {
	if s == nil || s.admissions == nil || s.destinations == nil || s.teleport == nil {
		return DimensionHoleAdmission{}, ErrDimensionHoleEntryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	players, err := s.admissions.EntryPlayers(portalUID, player)
	if err != nil {
		return DimensionHoleAdmission{}, err
	}
	type stagedEntry struct {
		player      *model.Player
		admission   DimensionHoleAdmission
		destination DimensionHoleEntryDestination
		created     bool
	}
	staged := make([]stagedEntry, 0, len(players))
	rollback := func(cause error) error {
		for index := len(staged) - 1; index >= 0; index-- {
			if staged[index].created {
				cause = errors.Join(cause, s.admissions.leave(staged[index].admission.PlayerUID, staged[index].admission.PortalUID))
			}
		}
		return cause
	}
	for _, member := range players {
		admission, created, admitErr := s.admissions.admit(portalUID, member)
		if admitErr != nil {
			return DimensionHoleAdmission{}, rollback(admitErr)
		}
		destination, destinationErr := s.destinations.ResolveDimensionHoleEntry(ctx, admission)
		if destinationErr != nil {
			if created {
				_ = s.admissions.leave(admission.PlayerUID, admission.PortalUID)
			}
			return DimensionHoleAdmission{}, rollback(destinationErr)
		}
		staged = append(staged, stagedEntry{player: member, admission: admission, destination: destination, created: created})
	}
	var initiatingAdmission DimensionHoleAdmission
	for index, entry := range staged {
		if entry.player == player {
			initiatingAdmission = entry.admission
		}
		if teleportErr := s.teleport.SubmitDimensionHoleEntry(ctx, entry.player, entry.admission, entry.destination); teleportErr != nil {
			for remaining := index; remaining < len(staged); remaining++ {
				if staged[remaining].created {
					teleportErr = errors.Join(teleportErr, s.admissions.leave(staged[remaining].admission.PlayerUID, staged[remaining].admission.PortalUID))
				}
			}
			return DimensionHoleAdmission{}, teleportErr
		}
	}
	if initiatingAdmission.PlayerUID == 0 {
		return DimensionHoleAdmission{}, ErrDimensionHoleEntryUnavailable
	}
	return initiatingAdmission, nil
}
