package questscript

import (
	"fmt"
	"sort"
	"strings"
)

// Calls symbolically evaluates only the register operations required by the
// declarative quest chunks. Unknown instructions invalidate their destination
// register; no bytecode or native function is executed.
func Calls(prototype Prototype) []Call {
	registers := make([]Value, int(prototype.MaxStack))
	calls := make([]Call, 0)
	for _, instruction := range prototype.Code {
		opcode := instruction & 0x3f
		a := int((instruction >> 6) & 0xff)
		b := int((instruction >> 23) & 0x1ff)
		c := int((instruction >> 14) & 0x1ff)
		bx := int(instruction >> 14)
		if a >= len(registers) {
			continue
		}
		switch opcode {
		case 0: // MOVE
			if b < len(registers) {
				registers[a] = registers[b]
			}
		case 1: // LOADK
			if bx < len(prototype.Constants) {
				registers[a] = prototype.Constants[bx]
			}
		case 2: // LOADBOOL
			registers[a] = Value{Kind: ValueBool, Bool: b != 0}
		case 3: // LOADNIL
			for index := a; index <= b && index < len(registers); index++ {
				registers[index] = Value{Kind: ValueNil}
			}
		case 5: // GETGLOBAL
			if bx < len(prototype.Constants) && prototype.Constants[bx].Kind == ValueString {
				registers[a] = Value{Kind: ValueGlobal, String: prototype.Constants[bx].String}
			}
		case 28: // CALL
			callee := registers[a]
			if callee.Kind == ValueGlobal && b > 0 && a+b <= len(registers) && isQuestDeclarationCall(callee.String) {
				args := append([]Value(nil), registers[a+1:a+b]...)
				calls = append(calls, Call{Function: callee.String, Args: args})
			}
			resultCount := c - 1
			if c == 0 {
				resultCount = len(registers) - a
			}
			for index := a; index < a+resultCount && index < len(registers); index++ {
				registers[index] = Value{}
			}
		default:
			registers[a] = Value{}
		}
	}
	return calls
}

func isQuestDeclarationCall(name string) bool {
	return strings.HasPrefix(name, "Lua") || strings.HasPrefix(name, "InsertQuest")
}

// AllPrototypes returns every nested prototype in deterministic file order.
func AllPrototypes(chunk Chunk) []Prototype {
	result := make([]Prototype, 0)
	var visit func(Prototype)
	visit = func(prototype Prototype) {
		for _, child := range prototype.Prototypes {
			result = append(result, child)
			visit(child)
		}
	}
	visit(chunk.Main)
	return result
}

type Definition struct {
	CodeName     string `json:"codeName"`
	Source       string `json:"source"`
	Variant      int    `json:"variant"`
	Declarations []Call `json:"declarations"`
}

// Definitions selects prototypes that declare exactly one quest identity.
// Handler prototypes without LuaGetQuestID are intentionally excluded.
func Definitions(chunk Chunk) ([]Definition, error) {
	definitions := make([]Definition, 0)
	for _, prototype := range AllPrototypes(chunk) {
		calls := Calls(prototype)
		codeName := ""
		for _, call := range calls {
			if call.Function != "LuaGetQuestID" {
				continue
			}
			if codeName != "" || len(call.Args) != 1 || call.Args[0].Kind != ValueString || call.Args[0].String == "" {
				return nil, fmt.Errorf("%w: %s has malformed quest identity", ErrInvalidChunk, prototype.Source)
			}
			codeName = call.Args[0].String
		}
		if codeName == "" {
			continue
		}
		definitions = append(definitions, Definition{CodeName: codeName, Source: prototype.Source, Declarations: calls})
	}
	sort.SliceStable(definitions, func(i, j int) bool {
		if definitions[i].CodeName == definitions[j].CodeName {
			return definitions[i].Source < definitions[j].Source
		}
		return definitions[i].CodeName < definitions[j].CodeName
	})
	variants := make(map[string]int)
	for index := range definitions {
		definitions[index].Variant = variants[definitions[index].CodeName]
		variants[definitions[index].CodeName]++
	}
	return definitions, nil
}
