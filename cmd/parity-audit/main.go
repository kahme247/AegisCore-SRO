// Command parity-audit verifies the maintained vSRO 1.188 parity manifest
// against the local, read-only reverse-engineering catalogs.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/feature"
	"github.com/Harvi8/AegisCore/protocol/parity"
)

func main() {
	workspace := flag.String("workspace", ".", "path to the go-sro worktree")
	requireComplete := flag.Bool("require-complete", false, "fail unless every capability passes the parity gate")
	matrixJSON := flag.String("matrix-json", "", "optional path for the complete row-level matrix as JSON; use - for stdout")
	flag.Parse()

	manifest, err := parity.LoadVSRO188()
	if err != nil {
		fatal(err)
	}
	auditBinaries(*workspace, manifest)
	for _, catalog := range manifest.Catalogs {
		path := filepath.Join(*workspace, filepath.FromSlash(catalog.Source))
		file, openErr := os.Open(path)
		if openErr != nil {
			fatal(fmt.Errorf("open catalog %q: %w", catalog.ID, openErr))
		}
		audit, auditErr := parity.AuditCatalog(file, catalog)
		closeErr := file.Close()
		if auditErr != nil {
			fatal(auditErr)
		}
		if closeErr != nil {
			fatal(fmt.Errorf("close catalog %q: %w", catalog.ID, closeErr))
		}
		fmt.Printf("%s: %d unique entries (expected %d)\n", audit.ID, audit.Actual, audit.Expected)
		if !audit.Matches() {
			fatal(fmt.Errorf("catalog %q count mismatch", audit.ID))
		}
	}
	matrix := loadEvidenceMatrix(*workspace, manifest)
	sort.Slice(matrix, func(i, j int) bool { return matrix[i].ID < matrix[j].ID })
	if *matrixJSON != "" {
		writeMatrixJSON(*matrixJSON, matrix)
	}
	counts := make(map[parity.MatrixKind]int)
	missingCounts := make(map[string]int)
	unresolved := 0
	for _, entry := range matrix {
		counts[entry.Kind]++
		missing := entry.MissingRequired()
		if len(missing) > 0 {
			unresolved++
			for _, field := range missing {
				missingCounts[field]++
			}
		}
	}
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Printf("matrix %s: %d rows\n", kind, counts[parity.MatrixKind(kind)])
	}
	fmt.Printf("matrix unresolved: %d of %d rows\n", unresolved, len(matrix))
	missingFields := make([]string, 0, len(missingCounts))
	for field := range missingCounts {
		missingFields = append(missingFields, field)
	}
	sort.Strings(missingFields)
	for _, field := range missingFields {
		fmt.Printf("matrix missing %s: %d rows\n", field, missingCounts[field])
	}

	issues := manifest.Gate()
	fmt.Printf("parity gate: %d incomplete capabilities of %d\n", len(issues), len(manifest.Capabilities))
	if *requireComplete && (len(issues) > 0 || unresolved > 0) {
		if unresolved > 0 {
			fmt.Fprintf(os.Stderr, "matrix: %d rows lack required parity evidence or ownership\n", unresolved)
		}
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "%s: %s\n", issue.CapabilityID, issue.Reason)
		}
		os.Exit(1)
	}
}

func auditBinaries(workspace string, manifest parity.Manifest) {
	root := filepath.Join(workspace, "..", ".references", "testin-decompilation", "binaries")
	for _, binary := range manifest.Binaries {
		file := mustOpen(filepath.Join(root, binary.File))
		audit, err := parity.AuditBinary(file, binary)
		mustClose(file)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("binary %s: sha256 %s\n", audit.Service, audit.Actual)
		if !audit.Matches() {
			fatal(fmt.Errorf("binary %q hash mismatch: expected %s, actual %s", audit.File, audit.Expected, audit.Actual))
		}
	}
}

func writeMatrixJSON(path string, matrix []parity.MatrixEntry) {
	var writer io.Writer = os.Stdout
	var file *os.File
	if path != "-" {
		var err error
		file, err = os.Create(path)
		if err != nil {
			fatal(fmt.Errorf("create matrix output %q: %w", path, err))
		}
		defer mustClose(file)
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(matrix); err != nil {
		fatal(fmt.Errorf("encode matrix JSON: %w", err))
	}
}

func loadEvidenceMatrix(workspace string, manifest parity.Manifest) []parity.MatrixEntry {
	catalogPaths := make(map[string]string, len(manifest.Catalogs))
	for _, catalog := range manifest.Catalogs {
		catalogPaths[catalog.ID] = filepath.Join(workspace, filepath.FromSlash(catalog.Source))
	}
	opcodeFile := mustOpen(catalogPaths["opcodes"])
	opcodes, err := parity.LoadOpcodeEvidence(opcodeFile)
	mustClose(opcodeFile)
	if err != nil {
		fatal(err)
	}

	hashes := make(map[string]string, len(manifest.Binaries))
	for _, binary := range manifest.Binaries {
		hashes[binary.File] = binary.SHA256
	}
	sqlFile := mustOpen(catalogPaths["procedures"])
	contracts, err := parity.LoadSQLContracts(sqlFile, hashes)
	mustClose(sqlFile)
	if err != nil {
		fatal(err)
	}

	legacyRoot := filepath.Join(workspace, "VSRO_TestIn(EPVP)", "VSRO_TestIn", "VSRO_TestIn")
	configFile := mustOpen(filepath.Join(legacyRoot, "server.cfg"))
	configuration, err := parity.LoadLegacyServerConfig(configFile)
	mustClose(configFile)
	if err != nil {
		fatal(err)
	}
	auditFeatureMappings(configuration)
	scheduleFile := mustOpen(filepath.Join(legacyRoot, "schedule.cfg"))
	schedules, err := parity.LoadLegacySchedules(scheduleFile)
	mustClose(scheduleFile)
	if err != nil {
		fatal(err)
	}

	scriptPaths, err := filepath.Glob(filepath.Join(workspace, "Script", "VIETNAM", "*.sct"))
	if err != nil {
		fatal(fmt.Errorf("find script artifacts: %w", err))
	}
	artifacts := make([]parity.ScriptArtifact, 0, len(scriptPaths))
	for _, path := range scriptPaths {
		file := mustOpen(path)
		hash := sha256.New()
		size, copyErr := io.Copy(hash, file)
		mustClose(file)
		if copyErr != nil {
			fatal(fmt.Errorf("hash script artifact %q: %w", path, copyErr))
		}
		relative, relErr := filepath.Rel(workspace, path)
		if relErr != nil {
			fatal(fmt.Errorf("resolve script artifact %q: %w", path, relErr))
		}
		artifacts = append(artifacts, parity.ScriptArtifact{Path: filepath.ToSlash(relative), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: size})
	}
	scripts, err := parity.ScriptEntries(artifacts)
	if err != nil {
		fatal(err)
	}

	entities := parity.EntityFamilyEntries()
	integrations := parity.ExternalIntegrationEntries()
	matrix := make([]parity.MatrixEntry, 0, len(opcodes)+len(contracts)+len(configuration)+len(schedules)+len(scripts)+len(entities)+len(integrations))
	matrix = append(matrix, opcodes...)
	matrix = append(matrix, contracts...)
	matrix = append(matrix, configuration...)
	matrix = append(matrix, schedules...)
	matrix = append(matrix, scripts...)
	matrix = append(matrix, entities...)
	matrix = append(matrix, integrations...)
	return matrix
}

func auditFeatureMappings(configuration []parity.MatrixEntry) {
	evidence := make(map[string]struct{}, len(configuration))
	for _, entry := range configuration {
		evidence[entry.ID] = struct{}{}
	}
	mappings := feature.DirectiveMappings()
	if len(mappings) != len(evidence) {
		fatal(fmt.Errorf("legacy directive mappings=%d evidence rows=%d", len(mappings), len(evidence)))
	}
	for _, mapping := range mappings {
		id := "config:" + strings.ToLower(mapping.Service) + ":" + strings.ToLower(mapping.Directive)
		if _, exists := evidence[id]; !exists {
			fatal(fmt.Errorf("legacy directive mapping %q has no evidence row", id))
		}
		delete(evidence, id)
	}
	if len(evidence) != 0 {
		fatal(fmt.Errorf("%d legacy configuration rows lack directive mappings", len(evidence)))
	}
	fmt.Printf("feature directive mappings: %d rows\n", len(mappings))
}

func mustOpen(path string) *os.File {
	file, err := os.Open(path)
	if err != nil {
		fatal(fmt.Errorf("open %q: %w", path, err))
	}
	return file
}

func mustClose(file *os.File) {
	if err := file.Close(); err != nil {
		fatal(fmt.Errorf("close %q: %w", file.Name(), err))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
