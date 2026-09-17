package model

import "testing"

func TestFortressExpenditureReasonsMatchClientMap(t *testing.T) {
	for reason := FortressExpenditureBuff; reason <= FortressExpenditureMiscellaneous; reason++ {
		if !reason.Valid() {
			t.Fatalf("reason %d should be valid", reason)
		}
	}
	if FortressExpenditureReason(9).Valid() {
		t.Fatal("reason 9 should be invalid")
	}
}

func TestFortressRequestAndPeriodEnumsMatchWireContracts(t *testing.T) {
	for _, requestType := range []FortressRequestType{FortressRequestAttack, FortressRequestDefend} {
		if !requestType.Valid() {
			t.Fatalf("request type %d should be valid", requestType)
		}
	}
	if FortressRequestType(0).Valid() || FortressRequestType(3).Valid() {
		t.Fatal("accepted unsupported fortress request type")
	}
	for _, period := range []FortressPeriod{FortressPeriodDefault, FortressPeriodBattle, FortressPeriodApplication, FortressPeriodTax} {
		if !period.Valid() {
			t.Fatalf("period %d should be valid", period)
		}
	}
	if FortressPeriod(3).Valid() {
		t.Fatal("accepted unsupported fortress period")
	}
}

func TestFortressPeriodTransitionsFailClosed(t *testing.T) {
	allowed := [][2]FortressPeriod{
		{FortressPeriodDefault, FortressPeriodApplication},
		{FortressPeriodApplication, FortressPeriodBattle},
		{FortressPeriodBattle, FortressPeriodTax},
		{FortressPeriodTax, FortressPeriodDefault},
	}
	for _, transition := range allowed {
		if !validFortressPeriodTransition(transition[0], transition[1]) {
			t.Fatalf("transition %d -> %d should be allowed", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]FortressPeriod{
		{FortressPeriodDefault, FortressPeriodDefault},
		{FortressPeriodBattle, FortressPeriodApplication},
		{FortressPeriodTax, FortressPeriodBattle},
		{FortressPeriod(3), FortressPeriodDefault},
	} {
		if validFortressPeriodTransition(transition[0], transition[1]) {
			t.Fatalf("transition %d -> %d should be rejected", transition[0], transition[1])
		}
	}
}
