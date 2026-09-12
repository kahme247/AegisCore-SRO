package main

import (
	"fmt"
	"sort"

	"github.com/Harvi8/AegisCore/agent-server/questscript"
)

type retainedCoverageFamily struct {
	Imported int      `json:"imported"`
	Enabled  int      `json:"runtime_enabled"`
	Excluded []string `json:"excluded,omitempty"`
}

type retainedQuestKey struct {
	quest   string
	variant int
}

func (key retainedQuestKey) String() string { return fmt.Sprintf("%s/%d", key.quest, key.variant) }

func retainedQuestRuntimeCoverage(startConditions []questscript.StartCondition, policies []questscript.CompletionPolicy,
	dependencies []questscript.Dependency, jobConditions []questscript.JobStartCondition, raceConditions []questscript.RaceStartCondition,
	pointRewards []questscript.PointReward, kills []questscript.KillMonsterObjective, deliveriesEX []questscript.DeliverItemEXObjective,
	deliveries []questscript.DeliverItemObjective, dialogs []questscript.DialogObjective, playerKills []questscript.KillPlayerObjective,
	collections []questscript.CollectionItemObjective, extendedKills []questscript.ExtendedKillMonsterObjective,
	talismans []questscript.TalismanObjective, rewards []questscript.RewardDeclaration) map[string]retainedCoverageFamily {
	counts := make(map[retainedQuestKey]int)
	for _, objective := range kills {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range deliveriesEX {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range deliveries {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range dialogs {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range playerKills {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range collections {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range extendedKills {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}
	for _, objective := range talismans {
		counts[retainedQuestKey{objective.QuestCodeName, objective.Variant}]++
	}

	systemStart := make(map[retainedQuestKey]bool)
	for _, condition := range startConditions {
		systemStart[retainedQuestKey{condition.QuestCodeName, condition.Variant}] = condition.SystemRequired
	}
	jobs := make(map[retainedQuestKey][]questscript.JobStartCondition)
	for _, condition := range jobConditions {
		key := retainedQuestKey{condition.QuestCodeName, condition.Variant}
		jobs[key] = append(jobs[key], condition)
	}

	families := make(map[string]retainedCoverageFamily)
	add := func(family string, key retainedQuestKey, enabled bool) {
		entry := families[family]
		entry.Imported++
		if enabled {
			entry.Enabled++
		} else {
			entry.Excluded = append(entry.Excluded, key.String())
		}
		families[family] = entry
	}

	killGroups := make(map[retainedQuestKey][]questscript.KillMonsterObjective)
	for _, objective := range kills {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		killGroups[key] = append(killGroups[key], objective)
	}
	for key, group := range killGroups {
		add("kill_monster_quests", key, safeKillMonsterGroup(group, counts[key]))
	}
	for _, objective := range deliveriesEX {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("deliver_item_ex_quests", key, key.quest == "QNO_CH_SMITH_1" && key.variant == 0 && objective.MissionIndex == 0 && counts[key] == 1)
	}
	for _, objective := range deliveries {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("deliver_item_quests", key, objective.Variant == 0 && objective.MissionIndex == 0 && counts[key] == 1)
	}
	for _, objective := range dialogs {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("dialog_quests", key, objective.Variant == 0 && objective.MissionIndex == 0 && counts[key] == 1)
	}
	for _, objective := range playerKills {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		conditions := jobs[key]
		add("kill_player_quests", key, safeKillPlayerObjective(objective, counts[key]) && systemStart[key] && len(conditions) == 1 && safeJobStartCondition(conditions[0]))
	}
	for _, objective := range collections {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("collection_objectives", key, safeCollectionObjective(objective, counts[key]))
	}
	for _, objective := range extendedKills {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("extended_kill_objectives", key, safeExtendedKillObjective(objective, counts[key]))
	}
	for _, objective := range talismans {
		key := retainedQuestKey{objective.QuestCodeName, objective.Variant}
		add("talisman_objectives", key, safeTalismanObjective(objective, counts[key]))
	}
	for _, policy := range policies {
		add("completion_policies", retainedQuestKey{policy.QuestCodeName, policy.Variant}, safeCompletionPolicy(policy))
	}
	for _, dependency := range dependencies {
		add("dependency_groups", retainedQuestKey{dependency.QuestCodeName, dependency.Variant}, safeDependencyGroup(dependency))
	}
	for _, condition := range jobConditions {
		add("job_conditions", retainedQuestKey{condition.QuestCodeName, condition.Variant}, safeJobStartCondition(condition))
	}
	for _, condition := range raceConditions {
		add("race_conditions", retainedQuestKey{condition.QuestCodeName, condition.Variant}, safeRaceStartCondition(condition))
	}
	for _, reward := range pointRewards {
		add("point_rewards", retainedQuestKey{reward.QuestCodeName, reward.Variant}, safePointReward(reward))
	}
	for _, reward := range rewards {
		add("item_reward_declarations", retainedQuestKey{reward.QuestCodeName, reward.Variant}, reward.Variant == 0)
	}
	for name, entry := range families {
		sort.Strings(entry.Excluded)
		families[name] = entry
	}
	return families
}
