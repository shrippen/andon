package svcdata

// Test access to unexported limits (Go's export_test idiom).
const MaxEntries = maxEntries

// Entries is the size of the larger cache.
func Entries() int {
	memMu.Lock()
	defer memMu.Unlock()
	return max(len(mem), len(latest))
}
