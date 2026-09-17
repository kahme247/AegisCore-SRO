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

// Enter reserves capacity before submitting the world-loading transition. A
// newly-created reservation is released if staging fails; an idempotent retry
// never erases a previously successful admission.
func (s *DimensionHoleEntryService) Enter(ctx context.Context, portalUID uint32, player *model.Player) (DimensionHoleAdmission, error) {
	if s == nil || s.admissions == nil || s.destinations == nil || s.teleport == nil {
		return DimensionHoleAdmission{}, ErrDimensionHoleEntryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	admission, created, err := s.admissions.admit(portalUID, player)
	if err != nil {
		return DimensionHoleAdmission{}, err
	}
	destination, err := s.destinations.ResolveDimensionHoleEntry(ctx, admission)
	if err == nil {
		err = s.teleport.SubmitDimensionHoleEntry(ctx, player, admission, destination)
	}
	if err != nil {
		if created {
			err = errors.Join(err, s.admissions.leave(admission.PlayerUID, admission.PortalUID))
		}
		return DimensionHoleAdmission{}, err
	}
	return admission, nil
}
