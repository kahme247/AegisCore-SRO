package vsro188

// CompatibilityStatus describes how much of a recovered stock surface is
// safe for the maintained Agent to expose. It is deliberately separate from
// packet codec success: a codec can be exact while the corresponding domain
// transaction is not yet available.
type CompatibilityStatus uint8

const (
	CompatibilityImplemented CompatibilityStatus = iota + 1
	CompatibilityPartial
	CompatibilityFramingOnly
	CompatibilityFailClosed
)

// CompatibilityCoverage is the machine-readable implementation boundary for
// the item/inventory/mall reverse-engineering work. Operation is a printable
// discriminator because not every surface has one and the unresolved entries
// must not acquire invented business names.
type CompatibilityCoverage struct {
	Surface        string
	RequestOpcode  uint16
	ResponseOpcode uint16
	Operation      string
	Status         CompatibilityStatus
	DomainBoundary string
	Evidence       string
}

// ItemCompatibilityCoverage returns the current stock-protocol coverage
// ledger. Callers receive a fresh slice so tests and diagnostics cannot mutate
// the package-level contract.
func ItemCompatibilityCoverage() []CompatibilityCoverage {
	return []CompatibilityCoverage{
		{Surface: "inventory relocation", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x00", Status: CompatibilityImplemented, DomainBoundary: "InventoryRelocator transaction", Evidence: "binary-confirmed relocation envelope"},
		{Surface: "ground pickup", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x06", Status: CompatibilityImplemented, DomainBoundary: "GroundItemService plus inventory transaction", Evidence: "binary-confirmed slot-only completion"},
		{Surface: "ground drop", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x07", Status: CompatibilityImplemented, DomainBoundary: "ground-drop compensating transaction", Evidence: "binary-confirmed slot-only completion"},
		{Surface: "NPC economy", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x08 (8-byte context form)/0x09", Status: CompatibilityPartial, DomainBoundary: "EconomyRepository transaction", Evidence: "AegisCore preserves the 8-byte economy form; stock also uses 0x08 for a distinct 4-byte package-item sender"},
		{Surface: "package-item insertion", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x08 (4-byte body)", Status: CompatibilityFailClosed, DomainBoundary: "no maintained package catalog/insertion transaction", Evidence: "exact request framing is decoded and length-demultiplexed; stock package updater/SQL lifecycle is not exposed without a domain contract"},
		{Surface: "NPC buyback", RequestOpcode: 0x7168, ResponseOpcode: 0xB168, Operation: "0x22 completion via 0xB034", Status: CompatibilityImplemented, DomainBoundary: "EconomyRepository buyback transaction", Evidence: "dedicated buyback request; stock updater 0x22 is not treated as a 0x7034 request"},
		{Surface: "legacy item mall", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x12", Status: CompatibilityImplemented, DomainBoundary: "ItemMallRepository transaction", Evidence: "binary-confirmed legacy completion envelope"},
		{Surface: "COS inventory transfer", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x17", Status: CompatibilityImplemented, DomainBoundary: "COSInventoryRepository transaction", Evidence: "binary-confirmed COS transfer completion"},
		{Surface: "modern item mall", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x18", Status: CompatibilityPartial, DomainBoundary: "ItemMallRepository transaction; unresolved stock result semantics", Evidence: "request/response widths confirmed; result meaning remains pending"},
		{Surface: "specialty/job goods", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x2B", Status: CompatibilityFramingOnly, DomainBoundary: "no maintained specialty catalog or transaction", Evidence: "request and fixed completion framing only"},
		{Surface: "specialty/job goods sell", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x2C", Status: CompatibilityFailClosed, DomainBoundary: "no recovered request lifecycle", Evidence: "operation remains ambiguous in recovered evidence"},
		{Surface: "item use envelope", RequestOpcode: 0x704C, ResponseOpcode: 0xB04C, Status: CompatibilityPartial, DomainBoundary: "ItemUseRepository transaction", Evidence: "known consumable/COS/service branches integrated; callback matrix incomplete"},
		{Surface: "unresolved item-use callbacks", RequestOpcode: 0x704C, ResponseOpcode: 0xB04C, Operation: "TypeID3=3 callbacks", Status: CompatibilityFailClosed, DomainBoundary: "no matching runtime state/action contract", Evidence: "binary callback gates recovered without stable domain semantics"},
		{Surface: "skill withdrawal", RequestOpcode: 0x7202, ResponseOpcode: 0xB202, Operation: "0x7202", Status: CompatibilityImplemented, DomainBoundary: "SkillRestorationRepository transaction", Evidence: "exact nine-byte request, result/error envelopes, item consumption, skill-point refund, and shared skill persistence path"},
		{Surface: "mastery withdrawal", RequestOpcode: 0x7203, ResponseOpcode: 0xB203, Operation: "0x7203", Status: CompatibilityImplemented, DomainBoundary: "SkillRestorationRepository transaction", Evidence: "exact nine-byte request, result/error envelopes, item consumption, mastery dependency checks, and shared skill persistence path"},
		{Surface: "storage transfer", RequestOpcode: 0x3047, ResponseOpcode: 0x3048, Operation: "0x3049", Status: CompatibilityImplemented, DomainBoundary: "storage serializers and repositories", Evidence: "multipart framing confirmed; item body remains domain-owned"},
		{Surface: "alchemy envelopes", RequestOpcode: 0x7150, ResponseOpcode: 0x716A, Status: CompatibilityPartial, DomainBoundary: "AlchemyRepository transaction", Evidence: "fixed envelopes confirmed; class-specific bodies remain domain-owned"},
		{Surface: "magic-cube lifecycle", RequestOpcode: 0, ResponseOpcode: 0, Status: CompatibilityFailClosed, DomainBoundary: "no recovered live opcode or item-state contract", Evidence: "table vocabulary confirmed; complete lifecycle unresolved"},
		{Surface: "item entity notifications", RequestOpcode: 0, ResponseOpcode: 0x3036, Operation: "0x3036/0x3038/0x3039", Status: CompatibilityImplemented, DomainBoundary: "runtime entity publication", Evidence: "typed notification layouts confirmed"},
	}
}
