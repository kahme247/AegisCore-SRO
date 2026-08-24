package parity

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type MatrixKind string

const (
	MatrixOpcode      MatrixKind = "opcode-evidence"
	MatrixProcedure   MatrixKind = "sql-procedure"
	MatrixTable       MatrixKind = "sql-table"
	MatrixConfig      MatrixKind = "configuration-switch"
	MatrixSchedule    MatrixKind = "scheduled-job"
	MatrixScript      MatrixKind = "script-artifact"
	MatrixEntity      MatrixKind = "entity-family"
	MatrixIntegration MatrixKind = "external-integration"
)

// MatrixEntry is deliberately capable of representing unresolved evidence.
// Empty semantic fields are reported by MissingRequired rather than inferred.
type MatrixEntry struct {
	ID              string     `json:"id"`
	Kind            MatrixKind `json:"kind"`
	Name            string     `json:"name"`
	Opcode          *uint16    `json:"opcode,omitempty"`
	Services        []string   `json:"services,omitempty"`
	Source          string     `json:"source"`
	Locator         string     `json:"locator"`
	BinarySHA256    []string   `json:"binary_sha256,omitempty"`
	FunctionAddress string     `json:"function_address,omitempty"`
	Direction       string     `json:"direction,omitempty"`
	Phase           string     `json:"phase,omitempty"`
	SchemaObjects   []string   `json:"schema_objects,omitempty"`
	Confidence      string     `json:"confidence"`
	Owner           string     `json:"owner,omitempty"`
	Aggregate       string     `json:"aggregate,omitempty"`
	Repository      string     `json:"repository,omitempty"`
	Tests           []string   `json:"tests,omitempty"`
	Status          Status     `json:"status"`
	EnabledDefault  bool       `json:"enabled_default"`
	Blockers        []string   `json:"blockers,omitempty"`
}

type ScriptArtifact struct {
	Path   string
	SHA256 string
	Size   int64
}

func LoadOpcodeEvidence(reader io.Reader) ([]MatrixEntry, error) {
	records, header, err := readCSV(reader, "opcode catalog")
	if err != nil {
		return nil, err
	}
	required := []string{"opcode", "name", "source", "line", "evidence"}
	if err := requireColumns(header, required); err != nil {
		return nil, fmt.Errorf("opcode catalog: %w", err)
	}
	entries := make([]MatrixEntry, 0, len(records))
	for index, record := range records {
		rawOpcode := strings.TrimPrefix(strings.TrimSpace(record[header["opcode"]]), "0x")
		value, parseErr := strconv.ParseUint(rawOpcode, 16, 16)
		if parseErr != nil {
			return nil, fmt.Errorf("opcode catalog row %d: invalid opcode %q", index+2, rawOpcode)
		}
		opcode := uint16(value)
		name := strings.TrimSpace(record[header["name"]])
		source := strings.TrimSpace(record[header["source"]])
		line := strings.TrimSpace(record[header["line"]])
		entries = append(entries, MatrixEntry{
			ID:         fmt.Sprintf("opcode:0x%04X:evidence:%04d", opcode, index+1),
			Kind:       MatrixOpcode,
			Name:       name,
			Opcode:     &opcode,
			Source:     source,
			Locator:    "line " + line,
			Confidence: strings.TrimSpace(record[header["evidence"]]),
			Owner:      "protocol/vsro188",
			Status:     StatusUnknown,
			Blockers: []string{
				"service, direction, and lifecycle phase require dispatch or capture evidence",
				"function address and binary provenance are not present in the flat catalog",
			},
		})
	}
	return entries, nil
}

func LoadSQLContracts(reader io.Reader, binaryHashes map[string]string) ([]MatrixEntry, error) {
	records, header, err := readCSV(reader, "SQL contract catalog")
	if err != nil {
		return nil, err
	}
	if err := requireColumns(header, []string{"file", "kind", "object", "evidence_string"}); err != nil {
		return nil, fmt.Errorf("SQL contract catalog: %w", err)
	}
	type aggregate struct {
		kind     MatrixKind
		name     string
		services map[string]struct{}
		hashes   map[string]struct{}
		rows     []string
	}
	grouped := make(map[string]*aggregate)
	for index, record := range records {
		kindValue := strings.TrimSpace(record[header["kind"]])
		var kind MatrixKind
		switch kindValue {
		case "procedure":
			kind = MatrixProcedure
		case "table":
			kind = MatrixTable
		default:
			continue
		}
		name := strings.TrimSpace(record[header["object"]])
		if name == "" {
			continue
		}
		key := string(kind) + ":" + name
		item := grouped[key]
		if item == nil {
			item = &aggregate{kind: kind, name: name, services: make(map[string]struct{}), hashes: make(map[string]struct{})}
			grouped[key] = item
		}
		file := strings.TrimSpace(record[header["file"]])
		item.services[strings.TrimSuffix(file, ".exe")] = struct{}{}
		if hash := binaryHashes[file]; hash != "" {
			item.hashes[hash] = struct{}{}
		}
		item.rows = append(item.rows, strconv.Itoa(index+2))
	}

	entries := make([]MatrixEntry, 0, len(grouped))
	for key, item := range grouped {
		var schemaObjects []string
		if item.kind == MatrixTable {
			schemaObjects = []string{item.name}
		}
		entries = append(entries, MatrixEntry{
			ID:            key,
			Kind:          item.kind,
			Name:          item.name,
			Services:      sortedSet(item.services),
			Source:        "../.references/testin-decompilation/sql-contract-catalog.csv",
			Locator:       "rows " + strings.Join(item.rows, ","),
			BinarySHA256:  sortedSet(item.hashes),
			SchemaObjects: schemaObjects,
			Owner:         sqlContractOwner(sortedSet(item.services)),
			Confidence:    "embedded-string; call semantics pending",
			Status:        StatusEvidenced,
			Blockers: []string{
				"function call site and transaction semantics require primary evidence",
				"AegisCore aggregate owner and repository mapping are unassigned",
			},
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// LoadLegacyServerConfig inventories GameServer and ShardManager directives,
// including commented feature switches. It does not interpret their values.
func LoadLegacyServerConfig(reader io.Reader) ([]MatrixEntry, error) {
	lines, err := scanLines(reader)
	if err != nil {
		return nil, fmt.Errorf("read legacy server configuration: %w", err)
	}
	targets := map[string]bool{"SR_GameServer": true, "SR_ShardManager": true}
	current, pending := "", ""
	entries := make([]MatrixEntry, 0)
	for index, raw := range lines {
		text, commented := uncommentLegacyLine(raw, false)
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if current == "" {
			if targets[trimmed] {
				pending = trimmed
				continue
			}
			if pending != "" && trimmed == "{" {
				current, pending = pending, ""
			}
			continue
		}
		if trimmed == "}" {
			current = ""
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		name := fields[0]
		entries = append(entries, MatrixEntry{
			ID:             fmt.Sprintf("config:%s:%s", strings.ToLower(current), strings.ToLower(name)),
			Kind:           MatrixConfig,
			Name:           name,
			Services:       []string{current},
			Source:         "VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/server.cfg",
			Locator:        fmt.Sprintf("line %d", index+1),
			Confidence:     "supplied legacy configuration",
			Status:         StatusEvidenced,
			EnabledDefault: !commented && !isDisabledValue(strings.Join(fields[1:], " ")),
			Owner:          "agent-server/feature",
			Blockers:       []string{"AegisCore feature owner and evidence-approved value semantics are unassigned"},
		})
	}
	return entries, nil
}

// LoadLegacySchedules inventories enabled and block-commented schedule names.
func LoadLegacySchedules(reader io.Reader) ([]MatrixEntry, error) {
	lines, err := scanLines(reader)
	if err != nil {
		return nil, fmt.Errorf("read legacy schedule configuration: %w", err)
	}
	inBlockComment := false
	entries := make([]MatrixEntry, 0)
	for index, raw := range lines {
		wasInBlock := inBlockComment
		trimmedRaw := strings.TrimSpace(raw)
		if strings.Contains(trimmedRaw, "/*") {
			inBlockComment = true
			trimmedRaw = strings.ReplaceAll(trimmedRaw, "/*", "")
		}
		lineCommented := wasInBlock || inBlockComment
		if strings.Contains(trimmedRaw, "*/") {
			trimmedRaw = strings.ReplaceAll(trimmedRaw, "*/", "")
			inBlockComment = false
		}
		text, slashCommented := uncommentLegacyLine(trimmedRaw, lineCommented)
		fields := strings.Fields(text)
		if len(fields) < 2 || fields[0] != "Schedule" {
			continue
		}
		name := fields[1]
		entries = append(entries, MatrixEntry{
			ID:             "schedule:" + strings.ToLower(name),
			Kind:           MatrixSchedule,
			Name:           name,
			Services:       []string{"SR_GameServer", "SR_ShardManager"},
			Source:         "VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/schedule.cfg",
			Locator:        fmt.Sprintf("line %d", index+1),
			Confidence:     "supplied legacy schedule definition",
			Status:         StatusEvidenced,
			EnabledDefault: !(lineCommented || slashCommented),
			Owner:          "framework/scheduler",
			Blockers:       []string{"handler ownership, missed-run behavior, and exact timezone semantics are unassigned"},
		})
	}
	return entries, nil
}

func ScriptEntries(artifacts []ScriptArtifact) ([]MatrixEntry, error) {
	entries := make([]MatrixEntry, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) == "" || !validSHA256(artifact.SHA256) || artifact.Size <= 0 {
			return nil, fmt.Errorf("invalid script artifact %q", artifact.Path)
		}
		entries = append(entries, MatrixEntry{
			ID:         "script:" + strings.ToLower(strings.ReplaceAll(artifact.Path, "\\", "/")),
			Kind:       MatrixScript,
			Name:       artifact.Path,
			Source:     artifact.Path,
			Locator:    fmt.Sprintf("whole file; %d bytes", artifact.Size),
			Confidence: "supplied compiled Lua 5.1 artifact",
			Owner:      "agent-server/quest-runtime",
			Status:     StatusBlocked,
			Blockers:   []string{"compiled Lua entrypoints and bytecode semantics require deterministic recovery"},
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, nil
}

// EntityFamilyEntries records every spawn family currently named by the
// recovered/current corpus. These are inventory obligations, not validated
// serializer layouts.
func EntityFamilyEntries() []MatrixEntry {
	names := []string{
		"player",
		"normal-npc",
		"mob-normal",
		"mob-champion",
		"mob-unique",
		"mob-giant",
		"cos-transport",
		"cos-growth-pet",
		"cos-ability-pet",
		"cos-guild",
		"fortress-cos",
		"fortress-structure",
		"dropped-equipment",
		"dropped-gold",
		"dropped-trade-item",
		"dropped-quest-item",
		"portal-or-store",
		"event-object",
	}
	entries := make([]MatrixEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, MatrixEntry{
			ID:         "entity:" + name,
			Kind:       MatrixEntity,
			Name:       name,
			Services:   []string{"SR_GameServer"},
			Source:     "Full SR_GameServer and SR_ShardManager Parity Program",
			Locator:    "workstream 3 exact spawn/despawn codec acceptance list: " + name,
			Confidence: "required inventory; primary TestIn serializer evidence pending",
			Owner:      "agent-server/world",
			Status:     StatusBlocked,
			Blockers: []string{
				"recover TestIn dispatch and serializer function address",
				"add sanitized stock-client spawn and despawn capture",
			},
		})
	}
	return entries
}

// ExternalIntegrationEntries distinguishes legacy integrations that require a
// secure replacement from gameplay capabilities that are simply missing.
func ExternalIntegrationEntries() []MatrixEntry {
	return []MatrixEntry{
		externalEntry("billing-http", "legacy billing HTTP", "server.cfg BILLING_SERVER_URL", "authenticated billing adapter plus outbox"),
		externalEntry("crest-ftp", "legacy guild-crest FTP", "server.cfg CREST_FTP_URL", "content-addressed authenticated storage adapter"),
		externalEntry("smc-administration", "legacy SMC administrative mutation surface", "sql-contract-catalog.csv _SMC_* procedures", "private mTLS management API with RBAC and audit"),
		externalEntry("web-fortress-status", "legacy fortress web-status publication", "sql-contract-catalog.csv _SiegeFortressStatusUpdateForWeb", "authenticated reporting adapter plus outbox"),
		externalEntry("shardmanager-runtime-link", "legacy GameServer-to-ShardManager authoritative dependency", "baseline-review.md confirmed architectural evidence", "consolidated Agent ownership with lifecycle and transactional repositories"),
	}
}

func externalEntry(id, name, locator, replacement string) MatrixEntry {
	return MatrixEntry{
		ID:         "integration:" + id,
		Kind:       MatrixIntegration,
		Name:       name,
		Services:   []string{"SR_GameServer", "SR_ShardManager"},
		Source:     "../.references/testin-decompilation",
		Locator:    locator,
		Confidence: "configuration, SQL-string, or binary diagnostic evidence",
		Owner:      externalOwner(id),
		Status:     StatusEvidenced,
		Blockers:   []string{"secure replacement is not fully implemented and certified: " + replacement},
	}
}

func (entry MatrixEntry) MissingRequired() []string {
	missing := make([]string, 0)
	for name, value := range map[string]string{
		"ID": entry.ID, "name": entry.Name, "source": entry.Source, "locator": entry.Locator,
		"confidence": entry.Confidence, "owner": entry.Owner,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(entry.Tests) == 0 {
		missing = append(missing, "tests")
	}
	if entry.Kind == MatrixOpcode {
		if len(entry.Services) == 0 {
			missing = append(missing, "service")
		}
		if entry.Direction == "" {
			missing = append(missing, "direction")
		}
		if entry.Phase == "" {
			missing = append(missing, "phase")
		}
	}
	if entry.Kind == MatrixProcedure || entry.Kind == MatrixTable || entry.Kind == MatrixOpcode {
		if len(entry.BinarySHA256) == 0 {
			missing = append(missing, "binary_sha256")
		}
		if entry.FunctionAddress == "" {
			missing = append(missing, "function_address")
		}
	}
	if entry.Kind == MatrixProcedure || entry.Kind == MatrixTable {
		if strings.TrimSpace(entry.Aggregate) == "" {
			missing = append(missing, "aggregate")
		}
		if len(entry.SchemaObjects) == 0 {
			missing = append(missing, "schema_objects")
		}
		if strings.TrimSpace(entry.Repository) == "" {
			missing = append(missing, "repository")
		}
	}
	sort.Strings(missing)
	return missing
}

func sqlContractOwner(services []string) string {
	for _, service := range services {
		switch service {
		case "SR_GameServer", "SR_ShardManager", "AgentServer":
			return "agent-server"
		}
	}
	for _, service := range services {
		if service == "GatewayServer" {
			return "gateway-server"
		}
		if service == "DownloadServer" {
			return "download-server"
		}
	}
	return "management/adapters"
}

func externalOwner(id string) string {
	switch id {
	case "smc-administration":
		return "agent-server/management"
	case "shardmanager-runtime-link":
		return "agent-server/runtime"
	default:
		return "framework/adapters"
	}
}

func readCSV(reader io.Reader, name string) ([][]string, map[string]int, error) {
	if reader == nil {
		return nil, nil, fmt.Errorf("%s reader is nil", name)
	}
	csvReader := csv.NewReader(reader)
	headerRecord, err := csvReader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read %s header: %w", name, err)
	}
	header := make(map[string]int, len(headerRecord))
	for index, value := range headerRecord {
		header[value] = index
	}
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", name, err)
	}
	return records, header, nil
}

func requireColumns(header map[string]int, columns []string) error {
	for _, column := range columns {
		if _, exists := header[column]; !exists {
			return fmt.Errorf("missing column %q", column)
		}
	}
	return nil
}

func scanLines(reader io.Reader) ([]string, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func uncommentLegacyLine(raw string, alreadyCommented bool) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	commented := alreadyCommented
	if strings.HasPrefix(trimmed, "//") {
		commented = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
	}
	if index := strings.Index(trimmed, "//"); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	return trimmed, commented
}

func isDisabledValue(value string) bool {
	trimmed := strings.TrimSpace(strings.Trim(value, `"`))
	return trimmed == "0" || strings.EqualFold(trimmed, "off")
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
