package svcdata

// Test access to unexported limits (Go's export_test idiom).
const MaxEntries = maxEntries

func Entries() int { return entries() }
