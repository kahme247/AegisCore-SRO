// Package vsro188 contains domain values, the typed protocol registry, and
// codecs for stabilized implementation layouts for the pinned vSRO 1.188
// target.
//
// The evidence-aware registry in this package identifies only the packet
// surface already used by listener admission. It does not establish packet
// layouts or stock-client parity. Codec golden tests prevent implementation
// drift, but the profile remains evidence-pending until sanitized stock-client
// captures validate those bytes.
package vsro188
