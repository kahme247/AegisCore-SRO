package feature

import (
	"testing"
)

func TestCatalogIsUniqueAndUnavailableCapabilitiesDefaultOff(t *testing.T) {
	if err := ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog() error = %v", err)
	}
	if got, want := len(Definitions()), 25; got != want {
		t.Fatalf("definition count = %d, want %d", got, want)
	}
	for _, definition := range Definitions() {
		if !definition.Available && definition.DefaultEnabled {
			t.Fatalf("unavailable feature %q defaults enabled", definition.Key)
		}
	}
}

func TestDirectiveMappingsCoverExactRecoveredInventory(t *testing.T) {
	mappings := DirectiveMappings()
	if got, want := len(mappings), 30; got != want {
		t.Fatalf("directive mapping count = %d, want %d", got, want)
	}
	seen := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		key := mapping.Service + "." + mapping.Directive
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate directive mapping %q", key)
		}
		seen[key] = struct{}{}
		if mapping.Class == FeatureSwitch && mapping.Feature == "" {
			t.Fatalf("feature directive %q has no feature key", key)
		}
		if mapping.AegisOwner == "" {
			t.Fatalf("directive %q has no AegisCore owner", key)
		}
	}
}

func TestResolveRejectsAttemptToEnableUnavailableFeature(t *testing.T) {
	provider := boolMap{"agent.features.open_market.enabled": true}
	if _, err := Resolve(provider); err == nil {
		t.Fatal("Resolve accepted unavailable open market feature")
	}
	set, err := Resolve(boolMap{})
	if err != nil {
		t.Fatalf("Resolve disabled defaults: %v", err)
	}
	for _, definition := range Definitions() {
		if set.Enabled(definition.Key) {
			t.Fatalf("feature %q unexpectedly enabled", definition.Key)
		}
	}
}

func TestResolveAcceptsCertifiedWinterEventFeature(t *testing.T) {
	set, err := Resolve(boolMap{"agent.features.winter_event_2009.enabled": true})
	if err != nil {
		t.Fatalf("Resolve Winter Event feature: %v", err)
	}
	if !set.Enabled(WinterEvent2009) {
		t.Fatal("Winter Event feature was not enabled")
	}
	for _, definition := range Definitions() {
		if definition.Key == WinterEvent2009 && (!definition.Available || len(definition.Evidence) == 0) {
			t.Fatalf("Winter Event definition is not evidence-certified: %#v", definition)
		}
	}
}

func TestResolveAcceptsCertifiedSeasonalAndServerEvents(t *testing.T) {
	set, err := Resolve(boolMap{
		"agent.features.server_event_system.enabled":  true,
		"agent.features.christmas_event_2007.enabled": true,
	})
	if err != nil {
		t.Fatalf("Resolve event features: %v", err)
	}
	if !set.Enabled(ServerEventSystem) || !set.Enabled(ChristmasEvent2007) {
		t.Fatalf("event features were not enabled: %#v", set)
	}
}

type boolMap map[string]bool

func (values boolMap) GetBool(key string) bool { return values[key] }
