package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

var packageItemColumns = []string{"ID", "CODE_NAME", "PAYMENT_DEVICE", "COST", "MEMBER_INDEX", "ItemID", "OPT_LEVEL", "VARIANCE", "DATA", "MAG_PARAM_NUM", "Stack_Size", "Bionic", "TypeID_1", "TypeID_2", "TypeID_3", "TypeID_4"}

func TestPackageItemRepositoryResolvesActiveNPCMultiMemberVector(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(resolvePackageItemQuery, catalogRepositoryTestResponse{
		columns: packageItemColumns,
		rows: [][]driver.Value{
			{int64(74222), "PACKAGE_ITEM_SPECIAL_TEST", int64(1), int64(383), int64(28951), int64(2147), int64(0), int64(0), int64(20), int64(0), int64(40), int64(0), int64(3), int64(3), int64(8), int64(1)},
			{int64(74222), "PACKAGE_ITEM_SPECIAL_TEST", int64(1), int64(383), int64(28952), int64(2148), int64(1), int64(7), int64(3), int64(1), int64(50), int64(0), int64(3), int64(3), int64(8), int64(1)},
			{int64(74222), "PACKAGE_ITEM_SPECIAL_TEST", int64(1), int64(383), int64(28953), int64(2149), int64(2), int64(9), int64(1), int64(0), int64(10), int64(0), int64(3), int64(3), int64(8), int64(1)},
		},
	})
	repository, err := NewPackageItemRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	good, err := repository.ResolvePackageItem(context.Background(), 1234, 2, 7)
	if err != nil {
		t.Fatalf("ResolvePackageItem: %v", err)
	}
	if good.ID != 74222 || good.CodeName != "PACKAGE_ITEM_SPECIAL_TEST" || good.PaymentDevice != 1 || good.Cost != 383 || len(good.Members) != 3 || good.Members[0].Index != 28951 || good.Members[1].RefItemID != 2148 || good.Members[1].MagicParams != 1 || good.Members[2].Data != 1 {
		t.Fatalf("resolved package = %+v", good)
	}
	catalogRepositoryTestState.Lock()
	queries := append([]catalogRepositoryTestQuery(nil), catalogRepositoryTestState.queries...)
	catalogRepositoryTestState.Unlock()
	if len(queries) != 1 || len(queries[0].args) != 3 || queries[0].args[0].Value != int64(2) || queries[0].args[1].Value != int64(7) || queries[0].args[2].Value != int64(1234) {
		t.Fatalf("selector query = %#v", queries)
	}
}

func TestPackageItemRepositoryRejectsMissingAndAmbiguousAuthority(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		resetCatalogRepositoryTestState()
		setCatalogRepositoryTestResponse(resolvePackageItemQuery, catalogRepositoryTestResponse{columns: packageItemColumns})
		repository, _ := NewPackageItemRepository(openCatalogRepositoryTestDB(t))
		if _, err := repository.ResolvePackageItem(context.Background(), 1234, 0, 0); !errors.Is(err, ErrPackageItemNotFound) {
			t.Fatalf("missing error = %v", err)
		}
	})
	t.Run("ambiguous package", func(t *testing.T) {
		resetCatalogRepositoryTestState()
		setCatalogRepositoryTestResponse(resolvePackageItemQuery, catalogRepositoryTestResponse{
			columns: packageItemColumns,
			rows: [][]driver.Value{
				{int64(1), "PACKAGE_A", int64(1), int64(383), int64(1), int64(2147), int64(0), int64(0), int64(1), int64(0), int64(40), int64(0), int64(3), int64(3), int64(8), int64(1)},
				{int64(2), "PACKAGE_B", int64(1), int64(383), int64(2), int64(2148), int64(0), int64(0), int64(1), int64(0), int64(40), int64(0), int64(3), int64(3), int64(8), int64(1)},
			},
		})
		repository, _ := NewPackageItemRepository(openCatalogRepositoryTestDB(t))
		if _, err := repository.ResolvePackageItem(context.Background(), 1234, 0, 0); !errors.Is(err, ErrPackageItemConflict) {
			t.Fatalf("ambiguous error = %v", err)
		}
	})
}

func TestPackageItemRepositoryResolvesTokenPaymentPolicy(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(resolvePackageItemQuery, catalogRepositoryTestResponse{
		columns: packageItemColumns,
		rows:    [][]driver.Value{{int64(99), "PACKAGE_TOKEN", int64(256), int64(17), int64(1), int64(2147), int64(0), int64(0), int64(1), int64(0), int64(100), int64(0), int64(3), int64(3), int64(1), int64(1)}},
	})
	repository, _ := NewPackageItemRepository(openCatalogRepositoryTestDB(t))
	good, err := repository.ResolvePackageItem(context.Background(), 1234, 0, 0)
	if err != nil || good.PaymentDevice != 256 || good.Cost != 17 {
		t.Fatalf("token policy = %+v, err=%v", good, err)
	}
}

func TestPlanPackageTokenDebitConsumesSlotsInOrder(t *testing.T) {
	stacks := []packageTokenStack{{slot: 13, itemID: 1, quantity: 4}, {slot: 17, itemID: 2, quantity: 10}, {slot: 22, itemID: 3, quantity: 8}}
	debits, err := planPackageTokenDebit(stacks, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(debits) != 2 || debits[0].stack.slot != 13 || debits[0].remaining != 0 || debits[1].stack.slot != 17 || debits[1].remaining != 5 {
		t.Fatalf("debits = %+v", debits)
	}
	if _, err = planPackageTokenDebit(stacks, 23); !errors.Is(err, ErrPackageItemInsufficientPayment) {
		t.Fatalf("insufficient-token error = %v", err)
	}
}

func TestPackageTokenDeviceCodeNamesMatchStoredProcedure(t *testing.T) {
	want := map[uint32]string{64: "ITEM_ETC_SD_TOKEN_01", 128: "ITEM_ETC_SD_TOKEN_02", 256: "ITEM_ETC_SD_TOKEN_03", 512: "ITEM_ETC_SD_TOKEN_04", 1024: "ITEM_ETC_ARENA_COIN"}
	for device, codeName := range want {
		if got, ok := packageTokenCodeName(device); !ok || got != codeName {
			t.Fatalf("device %d = %q, %t", device, got, ok)
		}
	}
}

func TestExpandPackageItemMembersMatchesStockSlotAndStackRules(t *testing.T) {
	specialty := PackageItemMember{RefItemID: 2147, MaxStack: 40, TypeID1: 3, TypeID2: 3, TypeID3: 8, TypeID4: 1}
	t.Run("single expandable uses one slot and request stack", func(t *testing.T) {
		expanded, err := expandPackageItemMembers([]PackageItemMember{specialty}, 40)
		if err != nil || len(expanded) != 1 || expanded[0].stack != 40 {
			t.Fatalf("expanded = %+v, err=%v", expanded, err)
		}
	})
	t.Run("multi-member package repeats clamped count", func(t *testing.T) {
		expanded, err := expandPackageItemMembers([]PackageItemMember{specialty, specialty, specialty}, 7)
		if err != nil || len(expanded) != 15 {
			t.Fatalf("expanded count = %d, err=%v", len(expanded), err)
		}
		for _, item := range expanded {
			if item.stack != 7 {
				t.Fatalf("expanded stack = %d, want request count 7", item.stack)
			}
		}
	})
	t.Run("fixed package data is preserved", func(t *testing.T) {
		fixed := PackageItemMember{RefItemID: 1, Data: 100, MaxStack: 1000, TypeID1: 3, TypeID2: 3}
		expanded, err := expandPackageItemMembers([]PackageItemMember{fixed}, 4)
		if err != nil || len(expanded) != 4 || expanded[0].stack != 100 {
			t.Fatalf("expanded = %+v, err=%v", expanded, err)
		}
	})
	t.Run("stack overflow rejected", func(t *testing.T) {
		if _, err := expandPackageItemMembers([]PackageItemMember{specialty}, 41); !errors.Is(err, ErrPackageItemConflict) {
			t.Fatalf("overflow error = %v", err)
		}
	})
}
