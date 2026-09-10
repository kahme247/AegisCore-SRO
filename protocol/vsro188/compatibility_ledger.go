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
func JobCompatibilityCoverage() []CompatibilityCoverage {
	return []CompatibilityCoverage{
		{Surface: "tri-job enrollment", RequestOpcode: 0x70E1, ResponseOpcode: 0xB0E1, Status: CompatibilityImplemented, DomainBoundary: "SocialRepository enrollment transaction", Evidence: "stock request and response field widths plus SR_GameServer fee/lifecycle branches"},
		{Surface: "tri-job withdrawal", RequestOpcode: 0x70E2, ResponseOpcode: 0xB0E2, Status: CompatibilityImplemented, DomainBoundary: "SocialRepository history and withdrawal transaction", Evidence: "stock request and result envelope plus SR_GameServer reset path"},
		{Surface: "tri-job alias", RequestOpcode: 0x70E3, ResponseOpcode: 0xB0E3, Status: CompatibilityImplemented, DomainBoundary: "case-insensitive durable alias authority", Evidence: "stock action discriminator and conditional result tail"},
		{Surface: "tri-job ranking", RequestOpcode: 0x70E4, ResponseOpcode: 0xB0E4, Status: CompatibilityImplemented, DomainBoundary: "durable active/contribution rankings with zeroed client-ignored compatibility byte", Evidence: "client 0x0088A130 reads the kind-0 tail byte at 0x0088A23F and discards it before list insertion/UI notification"},
		{Surface: "tri-job reward outcome", RequestOpcode: 0x70E5, ResponseOpcode: 0xB0E5, Status: CompatibilityImplemented, DomainBoundary: "explicit weekly proportional-distribution maintenance action and exactly-once pending-gold claim", Evidence: "_UpdateTriangleJobWeekly formula recovered; backup has no SQL Agent job and SR_GameServer has no distribution-procedure call, so execution time is deliberately operator-owned"},
		{Surface: "tri-job previous info", RequestOpcode: 0x70E6, ResponseOpcode: 0xB0E6, Status: CompatibilityImplemented, DomainBoundary: "current and previous durable job snapshot", Evidence: "client-confirmed 13-byte success tail"},
		{Surface: "tri-job experience update", ResponseOpcode: 0x30E6, Status: CompatibilityImplemented, DomainBoundary: "active player JOB snapshot", Evidence: "client-confirmed type:u8 level:u8 exp:u32 reader"},
		{Surface: "tri-job player spawn/select state", ResponseOpcode: 0x3019, Status: CompatibilityImplemented, DomainBoundary: "online player snapshot", Evidence: "active types 1..3 include level; no-job discriminator 4 omits level"},
		{Surface: "tri-job safe trade", ResponseOpcode: 0x34D5, Status: CompatibilityImplemented, DomainBoundary: "transactional admission counter, active status, restore, delivery, and scale reset", Evidence: "SR_GameServer 0x0051A960/0x0060E800/0x0060E890 plus pinned TRADE_SAFETY_OPTION/NUM values"},
	}
}

func ItemCompatibilityCoverage() []CompatibilityCoverage {
	return []CompatibilityCoverage{
		{Surface: "inventory relocation", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x00", Status: CompatibilityImplemented, DomainBoundary: "InventoryRelocator transaction", Evidence: "binary-confirmed relocation envelope"},
		{Surface: "ground pickup", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x06", Status: CompatibilityImplemented, DomainBoundary: "GroundItemService plus inventory transaction", Evidence: "binary-confirmed slot-only completion"},
		{Surface: "ground drop", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x07", Status: CompatibilityImplemented, DomainBoundary: "ground-drop compensating transaction", Evidence: "binary-confirmed slot-only completion"},
		{Surface: "NPC economy", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x08 (8-byte context form)/0x09", Status: CompatibilityPartial, DomainBoundary: "EconomyRepository transaction", Evidence: "AegisCore preserves the 8-byte economy form; stock also uses 0x08 for a distinct 4-byte package-item sender"},
		{Surface: "package-item insertion", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x08 (4-byte body)", Status: CompatibilityImplemented, DomainBoundary: "selected-NPC shop/package/member authority and atomic gold purchase", Evidence: "stock 0x005266A0/0x00616780 plus _STRG_ADD_PACKAGEITEM_CORE slot/count rules"},
		{Surface: "NPC buyback", RequestOpcode: 0x7168, ResponseOpcode: 0xB168, Operation: "0x22 completion via 0xB034", Status: CompatibilityImplemented, DomainBoundary: "EconomyRepository buyback transaction", Evidence: "dedicated buyback request; stock updater 0x22 is not treated as a 0x7034 request"},
		{Surface: "legacy item mall", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x12", Status: CompatibilityImplemented, DomainBoundary: "ItemMallRepository transaction", Evidence: "binary-confirmed legacy completion envelope"},
		{Surface: "COS inventory transfer", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x1A/0x1B", Status: CompatibilityImplemented, DomainBoundary: "COSInventoryRepository transaction", Evidence: "binary-confirmed COS transfer completion with object ID and source/destination slots"},
		{Surface: "modern item mall", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x18", Status: CompatibilityPartial, DomainBoundary: "ItemMallRepository transaction; unresolved stock result semantics", Evidence: "request/response widths confirmed; result meaning remains pending"},
		{Surface: "token package purchase", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x2B", Status: CompatibilityImplemented, DomainBoundary: "selected payment-policy package insertion and atomic inventory-token debit", Evidence: "0x0050CE80/0x005266A0/0x00618FE0 and _STRG_ADD_PACKAGEITEM_CORE token code names, slot-order debit, and continuation rows"},
		{Surface: "invalid specialty operation", RequestOpcode: 0x7034, ResponseOpcode: 0xB034, Operation: "0x2C", Status: CompatibilityFailClosed, DomainBoundary: "not a stock v188 item-operation request", Evidence: "SR_GameServer functions 0x004D8BF0 and 0x004F76E0 bound their discriminators at 0x2B; 0x2C takes the default/assert path"},
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
