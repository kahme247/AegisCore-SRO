package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type dimensionHoleEntryCoordinatorFake struct {
	err         error
	calls       int
	player      *model.Player
	admission   DimensionHoleAdmission
	destination DimensionHoleEntryDestination
}

func (f *dimensionHoleEntryCoordinatorFake) SubmitDimensionHoleEntry(_ context.Context, player *model.Player, admission DimensionHoleAdmission, destination DimensionHoleEntryDestination) error {
	f.calls++
	f.player = player
	f.admission = admission
	f.destination = destination
	return f.err
}

type dimensionHoleEntryDestinationFake struct {
	destination DimensionHoleEntryDestination
	err         error
	calls       int
	admission   DimensionHoleAdmission
}

func (f *dimensionHoleEntryDestinationFake) ResolveDimensionHoleEntry(_ context.Context, admission DimensionHoleAdmission) (DimensionHoleEntryDestination, error) {
	f.calls++
	f.admission = admission
	return f.destination, f.err
}

func TestDimensionHoleEntryServiceSubmitsAdmittedPlayer(t *testing.T) {
	admissions, player, instance := dimensionHoleEntryTestFixture(t)
	destination := DimensionHoleEntryDestination{WorldID: instance.Portal.Definition.WorldID, Region: 31201, X: 10, Y: 20, Z: 30}
	destinations := &dimensionHoleEntryDestinationFake{destination: destination}
	coordinator := &dimensionHoleEntryCoordinatorFake{}
	entries, err := NewDimensionHoleEntryService(admissions, destinations, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := entries.Enter(context.Background(), instance.Portal.GetUniqueID(), player)
	if err != nil {
		t.Fatalf("Enter() error = %v", err)
	}
	if destinations.calls != 1 || destinations.admission != admission ||
		coordinator.calls != 1 || coordinator.player != player || coordinator.admission != admission || coordinator.destination != destination {
		t.Fatalf("coordinator = calls:%d player:%p admission:%#v", coordinator.calls, coordinator.player, coordinator.admission)
	}
	if stored, ok := admissions.AdmissionForPlayer(player.GetUniqueID()); !ok || stored != admission {
		t.Fatalf("AdmissionForPlayer() = %#v, %t", stored, ok)
	}
}

func TestDimensionHoleEntryServiceTransfersOwnerPartyTogether(t *testing.T) {
	admissions, member, instance := dimensionHoleEntryTestFixture(t)
	players := admissions.players.(*dimensionHoleAdmissionPlayersFake)
	owner := players.byUID[instance.Portal.OwnerUniqueID]
	destination := DimensionHoleEntryDestination{WorldID: instance.Portal.Definition.WorldID, Region: 31201}
	destinations := &dimensionHoleEntryDestinationFake{destination: destination}
	coordinator := &dimensionHoleEntryCoordinatorFake{}
	entries, err := NewDimensionHoleEntryService(admissions, destinations, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := entries.Enter(context.Background(), instance.Portal.GetUniqueID(), owner)
	if err != nil {
		t.Fatalf("Enter(owner) error = %v", err)
	}
	if admission.PlayerUID != owner.GetUniqueID() || destinations.calls != 2 || coordinator.calls != 2 {
		t.Fatalf("party transfer admission=%#v destinationCalls=%d transferCalls=%d", admission, destinations.calls, coordinator.calls)
	}
	if _, ok := admissions.AdmissionForPlayer(owner.GetUniqueID()); !ok {
		t.Fatal("owner admission was not retained")
	}
	if _, ok := admissions.AdmissionForPlayer(member.GetUniqueID()); !ok {
		t.Fatal("party member admission was not retained")
	}
}

func TestDimensionHoleEntryServiceRollsBackOnlyNewAdmissionOnTransferFailure(t *testing.T) {
	admissions, player, instance := dimensionHoleEntryTestFixture(t)
	transferErr := errors.New("loading rejected")
	destinations := &dimensionHoleEntryDestinationFake{destination: DimensionHoleEntryDestination{WorldID: instance.Portal.Definition.WorldID, Region: 31201}}
	coordinator := &dimensionHoleEntryCoordinatorFake{err: transferErr}
	entries, _ := NewDimensionHoleEntryService(admissions, destinations, coordinator)
	if _, err := entries.Enter(context.Background(), instance.Portal.GetUniqueID(), player); !errors.Is(err, transferErr) {
		t.Fatalf("Enter(failing) error = %v", err)
	}
	if _, ok := admissions.AdmissionForPlayer(player.GetUniqueID()); ok {
		t.Fatal("failed first entry retained admission")
	}

	coordinator.err = nil
	admission, err := entries.Enter(context.Background(), instance.Portal.GetUniqueID(), player)
	if err != nil {
		t.Fatalf("Enter(success) error = %v", err)
	}
	coordinator.err = transferErr
	if _, err = entries.Enter(context.Background(), instance.Portal.GetUniqueID(), player); !errors.Is(err, transferErr) {
		t.Fatalf("Enter(retry failure) error = %v", err)
	}
	if stored, ok := admissions.AdmissionForPlayer(player.GetUniqueID()); !ok || stored != admission {
		t.Fatalf("retry failure erased existing admission: %#v, %t", stored, ok)
	}
}

func TestDimensionHoleEntryServiceRejectsMissingDependencies(t *testing.T) {
	coordinator := &dimensionHoleEntryCoordinatorFake{}
	destinations := &dimensionHoleEntryDestinationFake{}
	if entries, err := NewDimensionHoleEntryService(nil, destinations, coordinator); entries != nil || !errors.Is(err, ErrDimensionHoleEntryUnavailable) {
		t.Fatalf("NewDimensionHoleEntryService(nil admissions) = %#v, %v", entries, err)
	}
	admissions, _, _ := dimensionHoleEntryTestFixture(t)
	if entries, err := NewDimensionHoleEntryService(admissions, nil, coordinator); entries != nil || !errors.Is(err, ErrDimensionHoleEntryUnavailable) {
		t.Fatalf("NewDimensionHoleEntryService(nil destinations) = %#v, %v", entries, err)
	}
	if entries, err := NewDimensionHoleEntryService(admissions, destinations, nil); entries != nil || !errors.Is(err, ErrDimensionHoleEntryUnavailable) {
		t.Fatalf("NewDimensionHoleEntryService(nil coordinator) = %#v, %v", entries, err)
	}
}

func dimensionHoleEntryTestFixture(t *testing.T) (*DimensionHoleAdmissionService, *model.Player, DimensionHolePortalInstance) {
	t.Helper()
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
	member := dimensionHoleAdmissionTestPlayer(2, 200, int(definition.MinLevel))
	party := dimensionHoleAdmissionTestParty(owner.GetUniqueID(), owner, member)
	owner.Party = party
	member.Party = party
	instance := dimensionHoleAdmissionTestInstance(t, definition, owner, 501, 1)
	admissions, err := NewDimensionHoleAdmissionService(
		&dimensionHoleAdmissionInstancesFake{byUID: map[uint32]DimensionHolePortalInstance{501: instance}},
		&dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{100: owner, 200: member}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return admissions, member, instance
}
