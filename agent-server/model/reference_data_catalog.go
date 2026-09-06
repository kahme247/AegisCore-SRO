package model

// ReferenceDataCatalog is the immutable runtime view of one successfully
// loaded reference-data snapshot. It owns copies of the source maps and
// exposes values only, so neither legacy globals nor a caller retaining the
// snapshot can mutate the data used by long-running services.
//
// Item and character object IDs deliberately remain in separate namespaces:
// the database stores them in different catalogs, and consumers already know
// which kind of reference they need. A numerical overlap therefore cannot
// make an NPC lookup accidentally resolve an item.
type ReferenceDataCatalog struct {
	items      map[uint32]RefItem
	characters map[uint32]RefChar
	skills     map[uint32]RefSkill
}

// NewReferenceDataCatalog makes an isolated runtime catalog from a complete
// ReferenceDataSnapshot. The repository only returns a snapshot after every
// source catalog loads successfully; this constructor intentionally performs
// no I/O and does not publish any globals.
func NewReferenceDataCatalog(snapshot ReferenceDataSnapshot) *ReferenceDataCatalog {
	catalog := &ReferenceDataCatalog{
		items:      make(map[uint32]RefItem, len(snapshot.Items)),
		characters: make(map[uint32]RefChar, len(snapshot.Characters)),
		skills:     make(map[uint32]RefSkill, len(snapshot.Skills)),
	}
	for id, item := range snapshot.Items {
		catalog.items[id] = item
	}
	for id, character := range snapshot.Characters {
		catalog.characters[id] = character
	}
	for id, skill := range snapshot.Skills {
		catalog.skills[id] = skill
	}
	return catalog
}

// Skill returns a value copy of the requested skill reference.
func (c *ReferenceDataCatalog) Skill(id uint32) (RefSkill, bool) {
	if c == nil {
		return RefSkill{}, false
	}
	skill, ok := c.skills[id]
	return skill, ok
}

// SkillSnapshot returns value copies for rule engines that must inspect skill
// dependencies across the complete catalog. Callers cannot mutate the catalog.
func (c *ReferenceDataCatalog) SkillSnapshot() map[uint32]RefSkill {
	if c == nil {
		return nil
	}
	skills := make(map[uint32]RefSkill, len(c.skills))
	for id, skill := range c.skills {
		skills[id] = skill
	}
	return skills
}

// Item returns a value copy of the requested item reference.
func (c *ReferenceDataCatalog) Item(id uint32) (RefItem, bool) {
	if c == nil {
		return RefItem{}, false
	}
	item, ok := c.items[id]
	return item, ok
}

// Character returns a value copy of the requested character/NPC reference.
func (c *ReferenceDataCatalog) Character(id uint32) (RefChar, bool) {
	if c == nil {
		return RefChar{}, false
	}
	character, ok := c.characters[id]
	return character, ok
}
