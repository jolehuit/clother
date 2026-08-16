//go:build !unix

package config

// openNoFollow is 0 on platforms without O_NOFOLLOW. Those platforms are
// refused by cmd/clother before any of this runs; the constant only exists so
// that the refusal itself can be compiled and shipped.
const openNoFollow = 0
