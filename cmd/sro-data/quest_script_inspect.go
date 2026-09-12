package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/questscript"
)

var errQuestScriptInspectUsage = errors.New("usage: sro-data inspect-vsro-quest-script --script <Quest.sct> [--source <substring>] [--function <name>] [--extended-kill] [--kill-player] [--runtime-coverage]")

func inspectVSROQuestScript(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("sro-data inspect-vsro-quest-script", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scriptPath := flags.String("script", "", "Lua 5.1 quest chunk")
	sourceFilter := flags.String("source", "", "optional prototype source substring")
	functionFilter := flags.String("function", "", "optional declarative function name")
	showExtendedKill := flags.Bool("extended-kill", false, "print recovered extended-kill declarations as JSON")
	showKillPlayer := flags.Bool("kill-player", false, "print recovered kill-player declarations as JSON")
	showRuntimeCoverage := flags.Bool("runtime-coverage", false, "print retained objective and policy runtime admission as JSON")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *scriptPath == "" {
		return errQuestScriptInspectUsage
	}
	file, err := os.Open(*scriptPath)
	if err != nil {
		return err
	}
	defer file.Close()
	chunk, err := questscript.Decode(file)
	if err != nil {
		return err
	}
	prototypes := questscript.AllPrototypes(chunk)
	definitions, err := questscript.Definitions(chunk)
	if err != nil {
		return err
	}
	startConditions, err := questscript.StartConditions(definitions)
	if err != nil {
		return err
	}
	startMethods, err := questscript.StartMethods(definitions)
	if err != nil {
		return err
	}
	dependencies, err := questscript.Dependencies(definitions)
	if err != nil {
		return err
	}
	completionPolicies, err := questscript.CompletionPolicies(definitions)
	if err != nil {
		return err
	}
	jobStartConditions, err := questscript.JobStartConditions(definitions)
	if err != nil {
		return err
	}
	raceStartConditions, err := questscript.RaceStartConditions(definitions)
	if err != nil {
		return err
	}
	pointRewards, err := questscript.PointRewards(definitions)
	if err != nil {
		return err
	}
	killObjectives, err := questscript.KillMonsterObjectives(definitions)
	if err != nil {
		return err
	}
	deliverObjectives, err := questscript.DeliverItemEXObjectives(definitions)
	if err != nil {
		return err
	}
	standardDeliverObjectives, err := questscript.DeliverItemObjectives(definitions)
	if err != nil {
		return err
	}
	dialogObjectives, err := questscript.DialogObjectives(definitions)
	if err != nil {
		return err
	}
	killPlayerObjectives, err := questscript.KillPlayerObjectives(definitions)
	if err != nil {
		return err
	}
	collectionObjectives, err := questscript.CollectionItemObjectives(definitions)
	if err != nil {
		return err
	}
	extendedKillObjectives, err := questscript.ExtendedKillMonsterObjectives(definitions)
	if err != nil {
		return err
	}
	talismanObjectives, err := questscript.TalismanObjectives(definitions)
	if err != nil {
		return err
	}
	rewards, err := questscript.RewardDeclarations(definitions)
	if err != nil {
		return err
	}
	functionCounts := make(map[string]int)
	totalCalls := 0
	matched := make([]struct {
		Source string             `json:"source"`
		Calls  []questscript.Call `json:"calls"`
	}, 0)
	for _, prototype := range prototypes {
		calls := questscript.Calls(prototype)
		totalCalls += len(calls)
		for _, call := range calls {
			functionCounts[call.Function]++
		}
		if *sourceFilter != "" && strings.Contains(prototype.Source, *sourceFilter) {
			matched = append(matched, struct {
				Source string             `json:"source"`
				Calls  []questscript.Call `json:"calls"`
			}{prototype.Source, calls})
		}
	}
	functions := make([]string, 0, len(functionCounts))
	for name := range functionCounts {
		functions = append(functions, name)
	}
	sort.Strings(functions)
	if _, err = fmt.Fprintf(output, "decoded %d prototypes, %d quest definitions, %d start conditions, %d start methods, %d dependency groups, %d completion policies, %d job conditions, %d race conditions, %d point rewards, %d kill-monster objectives, %d deliver-item EX objectives, %d standard deliver-item objectives, %d dialog objectives, %d kill-player objectives, %d collection-item objectives, %d extended kill objectives, %d talisman objectives, %d rewards, and %d declarative calls\n", len(prototypes), len(definitions), len(startConditions), len(startMethods), len(dependencies), len(completionPolicies), len(jobStartConditions), len(raceStartConditions), len(pointRewards), len(killObjectives), len(deliverObjectives), len(standardDeliverObjectives), len(dialogObjectives), len(killPlayerObjectives), len(collectionObjectives), len(extendedKillObjectives), len(talismanObjectives), len(rewards), totalCalls); err != nil {
		return err
	}
	for _, name := range functions {
		if _, err = fmt.Fprintf(output, "%s %d\n", name, functionCounts[name]); err != nil {
			return err
		}
	}
	if *sourceFilter != "" {
		encoded, marshalErr := json.MarshalIndent(matched, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintln(output, string(encoded))
	}
	if *functionFilter != "" {
		matchedDefinitions := make([]questscript.Definition, 0)
		for _, definition := range definitions {
			filtered := definition
			filtered.Declarations = nil
			for _, call := range definition.Declarations {
				if call.Function == *functionFilter {
					filtered.Declarations = append(filtered.Declarations, call)
				}
			}
			if len(filtered.Declarations) != 0 {
				matchedDefinitions = append(matchedDefinitions, filtered)
			}
		}
		encoded, marshalErr := json.MarshalIndent(matchedDefinitions, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintln(output, string(encoded))
	}
	if *showExtendedKill {
		encoded, marshalErr := json.MarshalIndent(extendedKillObjectives, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintln(output, string(encoded))
	}
	if *showKillPlayer {
		encoded, marshalErr := json.MarshalIndent(killPlayerObjectives, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintln(output, string(encoded))
	}
	if *showRuntimeCoverage {
		coverage := retainedQuestRuntimeCoverage(startConditions, completionPolicies, dependencies, jobStartConditions,
			raceStartConditions, pointRewards, killObjectives, deliverObjectives, standardDeliverObjectives, dialogObjectives,
			killPlayerObjectives, collectionObjectives, extendedKillObjectives, talismanObjectives, rewards)
		encoded, marshalErr := json.MarshalIndent(coverage, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		_, err = fmt.Fprintln(output, string(encoded))
	}
	return err
}
