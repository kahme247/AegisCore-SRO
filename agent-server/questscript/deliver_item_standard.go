package questscript

import "fmt"

type DeliverItem struct {
	ItemCode string
	Quantity uint32
}

type DeliverItemObjective struct {
	QuestCodeName     string
	Variant           int
	MissionIndex      uint32
	ObjectiveKey      string
	TargetNPCCode     string
	Items             []DeliverItem
	TrailingParameter uint32
	DialogueKey       string
	Parameters        []Value
}

// DeliverItemObjectives recovers the variable-length LuaSetDeliverMissionData
// declarations. The item count is followed by quantity/code-name pairs and
// then two trailing declaration parameters. Their values are preserved even
// though only the final dialogue string has an evidenced identity.
func DeliverItemObjectives(definitions []Definition) ([]DeliverItemObjective, error) {
	objectives := make([]DeliverItemObjective, 0)
	for _, definition := range definitions {
		for _, call := range definition.Declarations {
			if call.Function != "LuaSetDeliverMissionData" {
				continue
			}
			if len(call.Args) < 10 || !globalValue(call.Args[0], "QUESTID") ||
				!globalValue(call.Args[2], "MISSION_TYPE_DELIVER_ITEM") || call.Args[3].Kind != ValueString ||
				call.Args[4].Kind != ValueString {
				return nil, fmt.Errorf("%w: %s variant %d has malformed deliver-item declaration", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			missionIndex, missionOK := unsignedInteger(call.Args[1])
			itemCount, countOK := unsignedInteger(call.Args[5])
			if !missionOK || !countOK || itemCount == 0 || uint64(len(call.Args)) != 8+uint64(itemCount)*2 ||
				call.Args[3].String == "" || call.Args[4].String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has invalid deliver-item identity or item count", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			items := make([]DeliverItem, 0, itemCount)
			for index := uint32(0); index < itemCount; index++ {
				offset := 6 + int(index)*2
				quantity, quantityOK := unsignedInteger(call.Args[offset])
				if !quantityOK || quantity == 0 || call.Args[offset+1].Kind != ValueString || call.Args[offset+1].String == "" {
					return nil, fmt.Errorf("%w: %s variant %d has malformed deliver item %d", ErrInvalidChunk, definition.CodeName, definition.Variant, index)
				}
				items = append(items, DeliverItem{ItemCode: call.Args[offset+1].String, Quantity: quantity})
			}
			trailingOffset := 6 + int(itemCount)*2
			trailing, trailingOK := unsignedInteger(call.Args[trailingOffset])
			dialogue := call.Args[trailingOffset+1]
			if !trailingOK || dialogue.Kind != ValueString || dialogue.String == "" {
				return nil, fmt.Errorf("%w: %s variant %d has malformed deliver-item trailing parameters", ErrInvalidChunk, definition.CodeName, definition.Variant)
			}
			objectives = append(objectives, DeliverItemObjective{
				QuestCodeName: definition.CodeName, Variant: definition.Variant, MissionIndex: missionIndex,
				ObjectiveKey: call.Args[3].String, TargetNPCCode: call.Args[4].String, Items: items,
				TrailingParameter: trailing, DialogueKey: dialogue.String, Parameters: append([]Value(nil), call.Args...),
			})
		}
	}
	return objectives, nil
}
