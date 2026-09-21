package handler

import (
	"context"

	"github.com/Harvi8/AegisCore/framework/security/auth"
)

func (h *LoginRequestHandler) issueAgentTicket(ctx context.Context, request LoginAdmissionRequest, traceID string) (uint32, error) {
	if h == nil || h.TicketIssuer == nil || h.TicketInstaller == nil || !request.SourceIP.IsValid() {
		return 0, auth.ErrAdmissionTicketInvalid
	}
	ticket, err := h.TicketIssuer.Issue(auth.AdmissionTicket{
		TraceID:      traceID,
		AccountID:    request.AccountID,
		Username:     request.Username,
		ShardID:      request.ShardID,
		ContentID:    request.ContentID,
		HasContentID: true,
		SourceIP:     request.SourceIP,
	})
	if err != nil {
		return 0, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	timeout := h.InstallTimeout
	if timeout <= 0 {
		timeout = auth.DefaultAdmissionTicketInstallTimeout
	}
	installCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := h.TicketInstaller.Install(installCtx, ticket); err != nil {
		// Gateway never exposed this QueueID to the client. Remove its issuer
		// copy immediately so an Agent outage cannot fill the local ticket map.
		h.TicketIssuer.Discard(ticket.QueueID)
		return 0, err
	}
	if err := h.TicketIssuer.Acknowledge(ticket.QueueID); err != nil {
		h.TicketIssuer.Discard(ticket.QueueID)
		return 0, err
	}
	// Agent owns the only ticket that can be redeemed. Keeping Gateway's
	// acknowledged issuer copy would consume its bounded map under login load.
	h.TicketIssuer.Discard(ticket.QueueID)
	return ticket.QueueID, nil
}
