package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/framework/scheduler"
	log "github.com/sirupsen/logrus"
)

func (a *App) handleSchedules(response http.ResponseWriter, request *http.Request) {
	if a.schedules == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	definitions, err := a.schedules.ListSchedules(ctx)
	if err != nil {
		log.WithError(err).Warn("read Agent schedules")
		writeAPIError(response, http.StatusServiceUnavailable, "Agent schedules are unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"schedules": definitions})
}

func (a *App) handleSchedule(response http.ResponseWriter, request *http.Request) {
	if a.schedules == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	id := strings.TrimSpace(request.PathValue("id"))
	if id == "" || len(id) > 128 {
		writeAPIError(response, http.StatusBadRequest, "invalid schedule ID")
		return
	}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	definition, err := a.schedules.GetSchedule(ctx, id)
	if err != nil {
		log.WithError(err).Warn("read Agent schedule")
		writeAPIError(response, http.StatusServiceUnavailable, "Agent schedule is unavailable")
		return
	}
	writeJSON(response, http.StatusOK, definition)
}

func (a *App) handleCreateSchedule(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin commands are not allowed")
		return
	}
	if a.schedules == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var definition scheduler.Definition
	if err := decoder.Decode(&definition); err != nil || definition.Version != 0 || !definition.NextRunAt.IsZero() || definition.Validate() != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid schedule definition")
		return
	}
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	event := AuditEvent{
		Action: "schedule.create", Outcome: "attempt", Operator: session.Username,
		RemoteAddr: clientIP(request.RemoteAddr), Target: "schedule:" + definition.ID,
		Details: map[string]any{"handler": definition.Handler, "recurrence": definition.Recurrence},
	}
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC schedule-create attempt")
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable; command was not sent")
		return
	}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	created, err := a.schedules.CreateSchedule(ctx, definition)
	if err != nil {
		event.Outcome = "failed"
		event.Details = map[string]any{"handler": definition.Handler, "failure": "agent_rejected"}
		a.recordBestEffort(event)
		log.WithError(err).Warn("create Agent schedule")
		writeAPIError(response, http.StatusBadRequest, "Agent rejected schedule definition")
		return
	}
	event.Outcome = "success"
	event.Details = map[string]any{"handler": created.Handler, "version": created.Version, "nextRunAt": created.NextRunAt}
	if err := a.audit.Record(event); err != nil {
		log.WithError(err).Error("record SMC schedule-create success")
		writeAPIError(response, http.StatusServiceUnavailable, "schedule was created but success audit could not be persisted")
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (a *App) handleUpdateSchedule(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin commands are not allowed")
		return
	}
	if a.schedules == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var command struct {
		ExpectedVersion uint64 `json:"expectedVersion"`
		Enabled         *bool  `json:"enabled"`
	}
	id := strings.TrimSpace(request.PathValue("id"))
	if err := decoder.Decode(&command); err != nil || id == "" || len(id) > 128 || command.ExpectedVersion == 0 || command.Enabled == nil {
		writeAPIError(response, http.StatusBadRequest, "invalid schedule update")
		return
	}
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	event := AuditEvent{
		Action: "schedule.enable", Outcome: "attempt", Operator: session.Username,
		RemoteAddr: clientIP(request.RemoteAddr), Target: "schedule:" + id,
		Details: map[string]any{"expectedVersion": command.ExpectedVersion, "enabled": *command.Enabled},
	}
	if err := a.audit.Record(event); err != nil {
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable; command was not sent")
		return
	}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	updated, err := a.schedules.SetScheduleEnabled(ctx, id, command.ExpectedVersion, *command.Enabled)
	if err != nil {
		event.Outcome = "failed"
		event.Details = map[string]any{"failure": "agent_rejected", "enabled": *command.Enabled}
		a.recordBestEffort(event)
		writeAPIError(response, http.StatusConflict, "Agent rejected schedule update")
		return
	}
	event.Outcome = "success"
	event.Details = map[string]any{"enabled": updated.Enabled, "version": updated.Version}
	if err := a.audit.Record(event); err != nil {
		writeAPIError(response, http.StatusServiceUnavailable, "schedule was updated but success audit could not be persisted")
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (a *App) handleScheduleRuns(response http.ResponseWriter, request *http.Request) {
	if a.schedules == nil {
		writeAPIError(response, http.StatusServiceUnavailable, "live Agent management channel is disabled")
		return
	}
	limit := 100
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeAPIError(response, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = parsed
	}
	ctx, cancel := contextWithTimeout(request, 4*time.Second)
	defer cancel()
	runs, err := a.schedules.ScheduleRuns(ctx, request.PathValue("id"), limit)
	if err != nil {
		log.WithError(err).Warn("read Agent schedule history")
		writeAPIError(response, http.StatusServiceUnavailable, "Agent schedule history is unavailable")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"runs": runs})
}
