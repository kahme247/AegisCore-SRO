package main

import (
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestSummarizeDimensionHoleReadinessCountsCompleteCatalogs(t *testing.T) {
	definitions := model.DimensionHoleCatalog()
	worldRows := make([]model.DimensionHoleWorldReference, 0, len(definitions))
	regionRows := make([]model.DimensionHoleWorldRegionReference, 0, 2088)
	for _, definition := range definitions {
		worldRows = append(worldRows, model.DimensionHoleWorldReference{
			WorldID: definition.WorldID, CodeName: definition.WorldCodeName, WorldType: model.DimensionHoleWorldType,
			MaxInstanceCount: model.DimensionHoleWorldMaxInstanceCount, MaxUserCount: definition.Capacity,
			WorldEntryType: model.DimensionHoleWorldEntryType, WorldEntranceType: model.DimensionHoleWorldEntranceType,
			WorldLeaveType: model.DimensionHoleWorldLeaveType, WorldDurationMS: model.DimensionHoleWorldDurationMS,
			WorldEmptyRemainMS: model.DimensionHoleWorldEmptyRemainMS, ConfigGroupCodeName: "GROUP_" + definition.WorldCodeName[4:],
		})
		count := 224
		if definition.WorldID <= 68 {
			count = 48
		} else if definition.WorldID <= 76 {
			count = 1
		}
		for index := 0; index < count; index++ {
			regionID := int16(int(definition.WorldID)*300 + index)
			if count == 1 {
				regionID = -32750
			}
			regionRows = append(regionRows, model.DimensionHoleWorldRegionReference{WorldID: definition.WorldID, RegionID: regionID})
		}
	}
	worlds, err := model.NewDimensionHoleWorldCatalog(worldRows)
	if err != nil {
		t.Fatal(err)
	}
	regions, err := model.NewDimensionHoleWorldRegionCatalog(regionRows)
	if err != nil {
		t.Fatal(err)
	}
	startRows := make([]model.DimensionHoleStartPositionReference, 0, len(definitions))
	for _, definition := range definitions {
		startRows = append(startRows, model.DimensionHoleStartPositionReference{WorldID: definition.WorldID, RegionID: regions.Regions(definition.WorldID)[0]})
	}
	starts, err := model.NewDimensionHoleStartPositionCatalog(regions, startRows)
	if err != nil {
		t.Fatal(err)
	}
	report := summarizeDimensionHoleReadiness(worlds, regions, starts)
	if report != (dimensionHoleReadinessReport{worlds: 22, regions: 2088, starts: 22}) {
		t.Fatalf("readiness report = %+v", report)
	}
}

func TestVerifyDimensionHoleRejectsArgumentsBeforeOpeningDatabase(t *testing.T) {
	if err := verifyDimensionHole(nil, []string{"unexpected"}, nil); !errors.Is(err, errDimensionHoleVerifyUsage) {
		t.Fatalf("verifyDimensionHole error = %v", err)
	}
}
