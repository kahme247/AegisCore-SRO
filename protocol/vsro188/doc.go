// Package vsro188 contains wire-format-neutral domain values for the pinned
// vSRO 1.88 target.
//
// This package intentionally does not define opcodes, packet layouts, or byte
// codecs. The repository's vSRO 1.88 profile is still evidence-pending and no
// sanitized stock-client capture corpus is available. Wire codecs must be
// added only alongside capture-backed tests; these types are the safe seam
// future codecs and authoritative game state can share in the meantime.
package vsro188
