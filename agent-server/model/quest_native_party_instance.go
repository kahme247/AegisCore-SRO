package model

type NativePartyInstanceHook struct {
	QuestID  int
	CodeName string
	Handler  uint32
}

var NativePartyInstanceHooks = [...]NativePartyInstanceHook{
	{311, "QNO_TQ_BAEKRYOUNG_17", 0x008D7270},
	{276, "QNO_TQ_STONEBOARD_4", 0x008D7270},
	{287, "QNO_TQ_ORNAMENT_4", 0x008D7270},
	{294, "QNO_TQ_GENERALORDER_4", 0x008D7270},
	{251, "QNO_CA_GORIA_7", 0x008D7270},
	{208, "QNO_EU_IVY_3", 0x008BBB40},
	{183, "QNO_EU_EASTEU_21", 0x008BBB40},
	{141, "QNO_RM_OLDWOMAN_6", 0x008BBB40},
}

func NativePartyInstanceHookForQuest(questID int) (NativePartyInstanceHook, bool) {
	for _, hook := range NativePartyInstanceHooks {
		if hook.QuestID == questID {
			return hook, true
		}
	}
	return NativePartyInstanceHook{}, false
}
