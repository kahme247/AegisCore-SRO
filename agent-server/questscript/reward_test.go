package questscript

import "testing"

func TestRewardDeclarationsRecoversBasicAndItemRewards(t *testing.T) {
	definitions := []Definition{
		{CodeName: "QNO_CH_SMITH_1", Declarations: []Call{{Function: "LuaSetAchievedItem", Args: []Value{
			{Kind: ValueNumber}, {Kind: ValueNumber, Number: 270}, {Kind: ValueNumber}, {Kind: ValueNumber, Number: 205}, {Kind: ValueNumber},
		}}}},
		{CodeName: "QNO_CH_SMITH_2", Declarations: []Call{{Function: "LuaSetAchievedItem", Args: []Value{
			{Kind: ValueNumber, Number: 1}, {Kind: ValueNumber, Number: 68860}, {Kind: ValueNumber, Number: 10000}, {Kind: ValueNumber},
			{Kind: ValueNumber, Number: 1}, {Kind: ValueString, String: "ITEM_QNO_CH_SMITH_2_01"}, {Kind: ValueNumber, Number: 1},
		}}}},
	}
	rewards, err := RewardDeclarations(definitions)
	if err != nil {
		t.Fatalf("RewardDeclarations: %v", err)
	}
	if len(rewards) != 2 || rewards[0].Experience != 270 || rewards[0].Gold != 205 || len(rewards[0].Items) != 0 {
		t.Fatalf("basic reward = %#v", rewards[0])
	}
	if rewards[1].Experience != 68860 || rewards[1].SkillExperience != 10000 || len(rewards[1].Items) != 1 || rewards[1].Items[0].ItemCode != "ITEM_QNO_CH_SMITH_2_01" {
		t.Fatalf("item reward = %#v", rewards[1])
	}
}
