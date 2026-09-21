package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/gateway-server/config"
	patchcatalog "github.com/Harvi8/AegisCore/patching/catalog"
	log "github.com/sirupsen/logrus"
)

// PatchRequest is the complete 0x6100 request. The request must be parsed in
// full before Gateway selects one exact configured profile.
type PatchRequest struct {
	ContentID  byte
	ModuleName string
	Version    uint32
}

const (
	patchResultSuccess            byte = 1
	patchResultFailure            byte = 2
	patchFailureUnknownClient     byte = 1
	patchFailureAvailable         byte = 2
	patchFailureNoAvailableServer byte = 3
	patchFailureLauncherNoServer  byte = 4
	patchFailureClientTooOld      byte = 5
)

// PatchRequestHandler loads explicit client profiles rather than hard-coding a
// client version. Each request must match exactly one configured tuple.
type PatchRequestHandler struct {
	LoadProfile  func() (config.ProtocolProfile, error)
	LoadProfiles func() ([]config.ProtocolProfile, error)
	Catalog      patchcatalog.Reader
	Channel      string
}

func NewPatchRequestHandler(loadProfile func() (config.ProtocolProfile, error)) *PatchRequestHandler {
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	return &PatchRequestHandler{LoadProfile: loadProfile}
}

func RegisterPatchRequestHandler(target *server.Server) {
	RegisterPatchRequestHandlerWithCatalog(target, nil, patchcatalog.StatusPublished)
}

func RegisterPatchRequestHandlerWithCatalog(target *server.Server, catalog patchcatalog.Reader, channel string) {
	if target == nil {
		return
	}
	handler := NewPatchRequestHandler(nil)
	handler.LoadProfiles = config.LoadProtocolProfiles
	handler.Catalog = catalog
	handler.Channel = channel
	target.RegisterHandler(opcode.PatchRequest, handler.Handle)
}

func (h *PatchRequestHandler) Handle(ctx context.Context, session *server.Session, packet network.Packet) error {
	request, err := parsePatchRequest(packet)
	if err != nil {
		log.Warnf("CLIENT_GATEWAY_PATCH_REQUEST rejected during parsing: %v", err)
		return err
	}
	log.Infof("CLIENT_GATEWAY_PATCH_REQUEST content_id=%d module=%q version=%d", request.ContentID, request.ModuleName, request.Version)
	profiles, err := h.loadProfiles()
	if err != nil {
		return fmt.Errorf("load gateway patch profile: %w", err)
	}
	response, outcome, selectedProfile, err := h.buildResponse(ctx, request, profiles)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("send patch response: missing session")
	}
	if selectedProfile != nil {
		session.UserContext.ProtocolProfile = selectedProfile.Name
	}
	if err := session.SendMassive(response); err != nil {
		return err
	}
	log.Infof("SERVER_GATEWAY_PATCH_RESPONSE sent %s massive=true", outcome)
	return nil
}

func (h *PatchRequestHandler) buildResponse(ctx context.Context, request PatchRequest, profiles []config.ProtocolProfile) (network.Packet, string, *config.ProtocolProfile, error) {
	var selected *config.ProtocolProfile
	for index := range profiles {
		profile := &profiles[index]
		if err := config.ValidateProtocolProfile(*profile); err != nil {
			return network.Packet{}, "", nil, fmt.Errorf("invalid pinned gateway profile: %w", err)
		}
		if request.ContentID == profile.ContentID && request.ModuleName == profile.ModuleName {
			selected = profile
			break
		}
	}
	if selected == nil || h.Catalog == nil {
		response, outcome, err := buildPatchResponseForProfiles(request, profiles)
		return response, outcome, selected, err
	}
	channel := h.Channel
	if channel == "" {
		channel = patchcatalog.StatusPublished
	}
	plan, err := h.Catalog.Resolve(ctx, request.ContentID, request.ModuleName, request.Version, channel)
	switch {
	case err == nil:
		if request.Version == plan.TargetVersion {
			response, responseErr := buildPatchSuccessResponse()
			return response, "result=1", selected, responseErr
		}
		response, responseErr := buildPatchAvailableResponse(plan)
		return response, fmt.Sprintf("result=2 reason=patch-available target=%d files=%d", plan.TargetVersion, len(plan.Files)), selected, responseErr
	case errors.Is(err, patchcatalog.ErrNoRelease):
		response, outcome, responseErr := buildPatchResponseForProfiles(request, profiles)
		return response, outcome, selected, responseErr
	case errors.Is(err, patchcatalog.ErrClientNewer):
		response, responseErr := buildPatchFailureResponse(patchFailureUnknownClient)
		return response, "result=2 reason=client-newer", selected, responseErr
	case errors.Is(err, patchcatalog.ErrClientTooOld):
		response, responseErr := buildPatchFailureResponse(patchFailureClientTooOld)
		return response, "result=2 reason=client-too-old", selected, responseErr
	case errors.Is(err, patchcatalog.ErrNoDownloadHost):
		response, responseErr := buildPatchFailureResponse(patchFailureLauncherNoServer)
		return response, "result=2 reason=no-download-server", selected, responseErr
	default:
		return network.Packet{}, "", selected, fmt.Errorf("resolve patch release: %w", err)
	}
}

func (h *PatchRequestHandler) loadProfiles() ([]config.ProtocolProfile, error) {
	if h.LoadProfiles != nil {
		return h.LoadProfiles()
	}
	loadProfile := h.LoadProfile
	if loadProfile == nil {
		loadProfile = config.LoadProtocolProfile
	}
	profile, err := loadProfile()
	if err != nil {
		return nil, err
	}
	return []config.ProtocolProfile{profile}, nil
}

func (request PatchRequest) Validate(profile config.ProtocolProfile) error {
	if err := config.ValidateProtocolProfile(profile); err != nil {
		return fmt.Errorf("invalid pinned gateway profile: %w", err)
	}
	if request.ContentID != profile.ContentID {
		return fmt.Errorf("unsupported patch content ID %d", request.ContentID)
	}
	if request.ModuleName != profile.ModuleName {
		return fmt.Errorf("unsupported patch module %q", request.ModuleName)
	}
	if request.Version != profile.Version {
		return fmt.Errorf("unsupported patch version %d", request.Version)
	}
	return nil
}

// buildPatchResponseForProfiles implements only TestIn branches whose response bytes and
// comparisons are confirmed in GatewayServer.exe. Exact versions succeed.
// Unknown clients and higher-than-known versions return [2,1]. Older clients
// return [2,3] because AegisCore does not yet advertise a verified patch server.
func buildPatchResponseForProfiles(request PatchRequest, profiles []config.ProtocolProfile) (network.Packet, string, error) {
	if len(profiles) == 0 {
		return network.Packet{}, "", fmt.Errorf("no gateway patch profiles configured")
	}
	var matchedClient bool
	var highestVersion uint32
	for _, profile := range profiles {
		if err := config.ValidateProtocolProfile(profile); err != nil {
			return network.Packet{}, "", fmt.Errorf("invalid pinned gateway profile: %w", err)
		}
		if request.ContentID != profile.ContentID || request.ModuleName != profile.ModuleName {
			continue
		}
		matchedClient = true
		if profile.Version > highestVersion {
			highestVersion = profile.Version
		}
		if request.Version == profile.Version {
			response, err := buildPatchSuccessResponse()
			return response, "result=1", err
		}
	}
	if !matchedClient || request.Version > highestVersion {
		response, err := buildPatchFailureResponse(patchFailureUnknownClient)
		return response, "result=2 reason=unknown-client", err
	}
	response, err := buildPatchFailureResponse(patchFailureNoAvailableServer)
	return response, "result=2 reason=no-available-patch-server", err
}
