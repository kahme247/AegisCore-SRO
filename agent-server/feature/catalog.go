// Package feature defines evidence-mapped optional legacy capabilities. A flag
// enables only its current AegisCore implementation; it never claims parity.
package feature

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Key string

const (
	WinterEvent2009       Key = "winter_event_2009"
	EUBusinessEvent       Key = "eu_business_event"
	GoldenPigEvent        Key = "golden_pig_february_event"
	ThanksgivingEvent     Key = "thanks_giving_event"
	LiberationEvent       Key = "liberation_event"
	OpenMarket            Key = "open_market"
	ServerEventSystem     Key = "server_event_system"
	ChristmasEvent2007    Key = "christmas_event_2007"
	FlagEvent             Key = "flag_event"
	BattleArenaRandom     Key = "battle_arena_random"
	BattleArenaParty      Key = "battle_arena_party"
	BattleArenaGuild      Key = "battle_arena_guild"
	BattleArenaJob        Key = "battle_arena_job"
	ArenaMatchOccupy      Key = "arena_match_occupy"
	ArenaMatchFlag        Key = "arena_match_flag"
	ArenaMatchPoint       Key = "arena_match_point"
	BillingAdapter        Key = "billing_adapter"
	CrestStorage          Key = "crest_storage"
	SpecialGoodsSchedules Key = "special_goods_schedules"
	UniqueSpawnSchedules  Key = "unique_spawn_schedules"
	MentorHonorSchedule   Key = "mentor_honor_schedule"
	FortressSchedules     Key = "fortress_schedules"
	FlagWorldSchedules    Key = "flag_world_schedules"
	QinTombGateSchedules  Key = "qin_tomb_gate_schedules"
	RocGateSchedules      Key = "roc_gate_schedules"
)

type Evidence struct {
	Source  string
	Locator string
}

type Definition struct {
	Key               Key
	ConfigKey         string
	Environment       string
	DefaultEnabled    bool
	LegacyEnabled     bool
	Available         bool
	UnavailableReason string
	Evidence          []Evidence
	LegacyDirectives  []string
}

type Set map[Key]bool

func (set Set) Enabled(key Key) bool { return set != nil && set[key] }

type BoolProvider interface {
	GetBool(string) bool
}

func Definitions() []Definition {
	definitions := []Definition{
		serverFlag(WinterEvent2009, "SR_GameServer.WINTER_EVENT_2009", false),
		serverFlag(EUBusinessEvent, "SR_GameServer.EUBUSINESS_EVENT", false),
		serverFlag(GoldenPigEvent, "SR_GameServer.GOLDEN_PIG_FEBRUARY_EVENT", false),
		serverFlag(ThanksgivingEvent, "SR_GameServer.THANKS_GIVING_EVENT", false),
		serverFlag(LiberationEvent, "SR_GameServer.LIBERATION_EVENT", false),
		serverFlag(OpenMarket, "SR_GameServer.SET_FEE_RATE", true, "SR_GameServer.SELL_FEE_RATE"),
		serverFlag(ServerEventSystem, "SR_ShardManager.SERVER_EVENT_SYSTEM", false),
		serverFlag(ChristmasEvent2007, "SR_ShardManager.ChristmasEvent2007", false),
		serverFlag(FlagEvent, "SR_ShardManager.FlagEvent", false),
		serverFlag(BattleArenaRandom, "SR_ShardManager.BattleArenaRandom", false),
		serverFlag(BattleArenaParty, "SR_ShardManager.BattleArenaParty", false),
		serverFlag(BattleArenaGuild, "SR_ShardManager.BattleArenaGuild", false),
		serverFlag(BattleArenaJob, "SR_ShardManager.BattleArenaJob", false),
		serverFlag(ArenaMatchOccupy, "SR_ShardManager.ArenaMatchOccupy", false),
		serverFlag(ArenaMatchFlag, "SR_ShardManager.ArenaMatchFlag", false),
		serverFlag(ArenaMatchPoint, "SR_ShardManager.ArenaMatchPoint", false),
		serverFlag(BillingAdapter, "SR_ShardManager.BILLING_SERVER_URL", true),
		serverFlag(CrestStorage, "SR_ShardManager.CREST_FTP_URL", true),
		scheduleFlag(SpecialGoodsSchedules, "InformSpecialGoodsSelling*/SpecialGoodsSelling*", true),
		scheduleFlag(UniqueSpawnSchedules, "UniqueSpawn_1..4", false),
		scheduleFlag(MentorHonorSchedule, "MentorHonorRecordUpdate", true),
		scheduleFlag(FortressSchedules, "AllowSiegeRequest/SiegeProgressing/FortressStatusUpdate/AllowSiegeTaxJob/AlertSiege*", true),
		scheduleFlag(FlagWorldSchedules, "FlagWorldParticipation*/FlagWorldNotice*/FlagWorldPlaying*", true),
		scheduleFlag(QinTombGateSchedules, "ChinsTombGateOpen*", true),
		scheduleFlag(RocGateSchedules, "RocTopGateOpen*", true),
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Key < definitions[j].Key })
	return definitions
}

func Resolve(provider BoolProvider) (Set, error) {
	if provider == nil {
		return nil, errors.New("feature configuration provider is required")
	}
	set := make(Set)
	for _, definition := range Definitions() {
		enabled := provider.GetBool(definition.ConfigKey)
		if enabled && !definition.Available {
			return nil, fmt.Errorf("feature %q cannot be enabled: %s", definition.Key, definition.UnavailableReason)
		}
		set[definition.Key] = enabled
	}
	return set, nil
}

func ValidateCatalog() error {
	seenKeys := make(map[Key]struct{})
	seenConfig := make(map[string]struct{})
	seenEnvironment := make(map[string]struct{})
	for _, definition := range Definitions() {
		if definition.Key == "" || definition.ConfigKey == "" || definition.Environment == "" || len(definition.Evidence) == 0 {
			return fmt.Errorf("feature %q is incomplete", definition.Key)
		}
		if !definition.Available && strings.TrimSpace(definition.UnavailableReason) == "" {
			return fmt.Errorf("feature %q lacks an unavailable reason", definition.Key)
		}
		if _, duplicate := seenKeys[definition.Key]; duplicate {
			return fmt.Errorf("duplicate feature key %q", definition.Key)
		}
		if _, duplicate := seenConfig[definition.ConfigKey]; duplicate {
			return fmt.Errorf("duplicate feature config key %q", definition.ConfigKey)
		}
		if _, duplicate := seenEnvironment[definition.Environment]; duplicate {
			return fmt.Errorf("duplicate feature environment %q", definition.Environment)
		}
		seenKeys[definition.Key] = struct{}{}
		seenConfig[definition.ConfigKey] = struct{}{}
		seenEnvironment[definition.Environment] = struct{}{}
	}
	for _, mapping := range DirectiveMappings() {
		if mapping.Class != FeatureSwitch {
			continue
		}
		if _, exists := seenKeys[mapping.Feature]; !exists {
			return fmt.Errorf("directive %s.%s maps to unknown feature %q", mapping.Service, mapping.Directive, mapping.Feature)
		}
	}
	return nil
}

type DirectiveClass string

const (
	FeatureSwitch  DirectiveClass = "feature"
	RuntimeSetting DirectiveClass = "setting"
	Replaced       DirectiveClass = "replaced"
)

type DirectiveMapping struct {
	Service       string
	Directive     string
	Class         DirectiveClass
	Feature       Key
	AegisOwner    string
	LegacyEnabled bool
}

// DirectiveMappings covers every directive inventoried from the supplied
// SR_GameServer and SR_ShardManager blocks, including non-feature settings.
func DirectiveMappings() []DirectiveMapping {
	return []DirectiveMapping{
		setting("SR_GameServer", "Certification", Replaced, "private admission/topology"),
		setting("SR_GameServer", "LOCALE", RuntimeSetting, "protocol profile vsro188"),
		setting("SR_GameServer", "ExpRatio", RuntimeSetting, "gameplay rate repository"),
		setting("SR_GameServer", "ExpRatioParty", RuntimeSetting, "party progression rates"),
		setting("SR_GameServer", "DropItemRatio", RuntimeSetting, "loot rate configuration"),
		setting("SR_GameServer", "DropGoldAmountCoef", RuntimeSetting, "loot rate configuration"),
		featureDirective("SR_GameServer", "WINTER_EVENT_2009", WinterEvent2009, false),
		featureDirective("SR_GameServer", "EUBUSINESS_EVENT", EUBusinessEvent, false),
		featureDirective("SR_GameServer", "GOLDEN_PIG_FEBRUARY_EVENT", GoldenPigEvent, false),
		featureDirective("SR_GameServer", "THANKS_GIVING_EVENT", ThanksgivingEvent, false),
		featureDirective("SR_GameServer", "LIBERATION_EVENT", LiberationEvent, false),
		featureDirective("SR_GameServer", "SET_FEE_RATE", OpenMarket, true),
		featureDirective("SR_GameServer", "SELL_FEE_RATE", OpenMarket, true),
		setting("SR_ShardManager", "Certification", Replaced, "consolidated Agent lifecycle"),
		featureDirective("SR_ShardManager", "BILLING_SERVER_URL", BillingAdapter, true),
		featureDirective("SR_ShardManager", "CREST_FTP_URL", CrestStorage, true),
		setting("SR_ShardManager", "LOCALE", RuntimeSetting, "protocol profile vsro188"),
		setting("SR_ShardManager", "ExtraExpRatio", RuntimeSetting, "gameplay rate repository"),
		featureDirective("SR_ShardManager", "SERVER_EVENT_SYSTEM", ServerEventSystem, false),
		featureDirective("SR_ShardManager", "ChristmasEvent2007", ChristmasEvent2007, false),
		featureDirective("SR_ShardManager", "FlagEvent", FlagEvent, false),
		setting("SR_ShardManager", "HourForMeterRateLevelFirst", RuntimeSetting, "persistent scheduler"),
		setting("SR_ShardManager", "HourForMeterRateLevelSecond", RuntimeSetting, "persistent scheduler"),
		featureDirective("SR_ShardManager", "BattleArenaRandom", BattleArenaRandom, false),
		featureDirective("SR_ShardManager", "BattleArenaParty", BattleArenaParty, false),
		featureDirective("SR_ShardManager", "BattleArenaGuild", BattleArenaGuild, false),
		featureDirective("SR_ShardManager", "BattleArenaJob", BattleArenaJob, false),
		featureDirective("SR_ShardManager", "ArenaMatchOccupy", ArenaMatchOccupy, false),
		featureDirective("SR_ShardManager", "ArenaMatchFlag", ArenaMatchFlag, false),
		featureDirective("SR_ShardManager", "ArenaMatchPoint", ArenaMatchPoint, false),
	}
}

func serverFlag(key Key, directive string, legacyEnabled bool, additional ...string) Definition {
	directives := append([]string{directive}, additional...)
	return unavailableDefinition(key, legacyEnabled, directives, Evidence{Source: "VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/server.cfg", Locator: strings.Join(directives, ", ")})
}

func scheduleFlag(key Key, locator string, legacyEnabled bool) Definition {
	return unavailableDefinition(key, legacyEnabled, nil, Evidence{Source: "VSRO_TestIn(EPVP)/VSRO_TestIn/VSRO_TestIn/schedule.cfg", Locator: locator})
}

func unavailableDefinition(key Key, legacyEnabled bool, directives []string, evidence Evidence) Definition {
	upper := strings.ToUpper(strings.ReplaceAll(string(key), "-", "_"))
	return Definition{
		Key: key, ConfigKey: "agent.features." + string(key) + ".enabled", Environment: "AGENT_FEATURE_" + upper + "_ENABLED",
		DefaultEnabled: false, LegacyEnabled: legacyEnabled, Available: false,
		UnavailableReason: "implementation and evidence certification are incomplete",
		Evidence:          []Evidence{evidence}, LegacyDirectives: directives,
	}
}

func setting(service, directive string, class DirectiveClass, owner string) DirectiveMapping {
	return DirectiveMapping{Service: service, Directive: directive, Class: class, AegisOwner: owner}
}

func featureDirective(service, directive string, feature Key, legacyEnabled bool) DirectiveMapping {
	return DirectiveMapping{Service: service, Directive: directive, Class: FeatureSwitch, Feature: feature, AegisOwner: "agent feature catalog", LegacyEnabled: legacyEnabled}
}
