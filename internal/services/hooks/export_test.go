package hooks

// Test access to the unexported limit (Go's export_test idiom).
const PerMinute = perMinute

// ResetFlood forgets the counted events: connection ids repeat across
// test databases.
func ResetFlood() {
	floodMu.Lock()
	defer floodMu.Unlock()
	clear(flood)
}
