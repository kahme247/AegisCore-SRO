package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

var mediaExportNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

type mediaExportCommand struct {
	Directory string `json:"directory"`
	Table     string `json:"table,omitempty"`
}

type mediaExportResult struct {
	Directory       string `json:"directory"`
	ServerDirectory string `json:"serverDirectory"`
	Output          string `json:"output"`
}

func (a *App) handleMediaExportConfig(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"root": mediaExportDisplayRoot(a.settings.MediaExportDisplayRoot)})
}

func (a *App) handleMediaExport(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		writeAPIError(response, http.StatusForbidden, "cross-origin commands are not allowed")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var command mediaExportCommand
	if err := decoder.Decode(&command); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid Media export command")
		return
	}
	command.Directory = strings.TrimSpace(command.Directory)
	command.Table = strings.TrimSpace(command.Table)
	if !mediaExportNamePattern.MatchString(command.Directory) || command.Directory == "." || command.Directory == ".." {
		writeAPIError(response, http.StatusBadRequest, "directory must be a 1..80 character name using letters, numbers, dot, dash, or underscore")
		return
	}
	if len(command.Table) > 128 || strings.ContainsAny(command.Table, "\x00\r\n") {
		writeAPIError(response, http.StatusBadRequest, "table filter is invalid")
		return
	}
	if !a.mediaExportMu.TryLock() {
		writeAPIError(response, http.StatusConflict, "another Media export is already running")
		return
	}
	defer a.mediaExportMu.Unlock()

	root, destination, err := prepareMediaExportDestination(a.settings.MediaExportRoot, command.Directory)
	if err != nil {
		writeAPIError(response, http.StatusConflict, err.Error())
		return
	}
	displayDestination := mediaExportDisplayDestination(a.settings.MediaExportDisplayRoot, command.Directory)
	session := request.Context().Value(sessionContextKey{}).(operatorSession)
	event := AuditEvent{
		Action: "media.export", Outcome: "attempt", Operator: session.Username,
		RemoteAddr: clientIP(request.RemoteAddr), Target: command.Directory,
		Details: map[string]any{"root": root, "displayDestination": displayDestination, "table": command.Table},
	}
	if err := a.audit.Record(event); err != nil {
		writeAPIError(response, http.StatusServiceUnavailable, "audit service unavailable; export was not started")
		return
	}

	arguments := []string{"media-sync", "--output", destination}
	if command.Table == "" {
		arguments = append(arguments, "--supported-only")
	} else {
		arguments = append(arguments, "--table", command.Table)
	}
	ctx, cancel := contextWithTimeout(request, 5*time.Minute)
	defer cancel()
	var commandOutput bytes.Buffer
	process := exec.CommandContext(ctx, a.settings.MediaExportBinary, arguments...)
	process.Stdout = &commandOutput
	process.Stderr = &commandOutput
	err = process.Run()
	if err != nil {
		event.Outcome = "failed"
		event.Details["failure"] = strings.TrimSpace(commandOutput.String())
		a.recordBestEffort(event)
		log.WithError(err).Warn("execute SMC Media export")
		writeAPIError(response, http.StatusUnprocessableEntity, mediaExportFailureMessage(ctx.Err(), commandOutput.String()))
		return
	}
	event.Outcome = "success"
	event.Details["destination"] = displayDestination
	if err := a.audit.Record(event); err != nil {
		writeAPIError(response, http.StatusServiceUnavailable, "Media files were exported but the success audit could not be persisted")
		return
	}
	writeJSON(response, http.StatusOK, mediaExportResult{Directory: displayDestination, ServerDirectory: destination, Output: strings.TrimSpace(commandOutput.String())})
}

func prepareMediaExportDestination(root, name string) (string, string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve Media export root: %w", err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", "", fmt.Errorf("create Media export root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("inspect Media export root: %w", err)
	}
	destination := filepath.Join(root, name)
	if _, err := os.Stat(destination); err == nil {
		return "", "", errors.New("the selected export directory already exists; choose a new name")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect Media export destination: %w", err)
	}
	return root, destination, nil
}

func mediaExportDisplayRoot(root string) string {
	root = strings.TrimRight(strings.TrimSpace(root), `/\`)
	if strings.Contains(root, `\`) {
		return strings.ReplaceAll(root, "/", `\`)
	}
	return root
}

func mediaExportDisplayDestination(root, name string) string {
	root = mediaExportDisplayRoot(root)
	if strings.Contains(root, `\`) {
		return root + `\` + name
	}
	return root + "/" + name
}

func mediaExportFailureMessage(contextErr error, output string) string {
	if contextErr != nil {
		return "Media export timed out or was cancelled"
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return "Media export failed"
	}
	if len(output) > 1000 {
		output = output[len(output)-1000:]
	}
	return output
}
