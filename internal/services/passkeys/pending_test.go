package passkeys

import (
	"strconv"
	"testing"
)

// TestLoginCeremoniesSpareRegistration: anonymous login starts may fill
// their own budget, but never keep a logged-in user from adding a key.
func TestLoginCeremoniesSpareRegistration(t *testing.T) {
	t.Cleanup(func() { pending = map[string]ceremony{} })
	for i := range maxPending {
		if err := keep("login-"+strconv.Itoa(i), ceremony{}); err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
	}
	if err := keep("login-more", ceremony{}); err != ErrBusy {
		t.Fatalf("login past the budget: %v", err)
	}
	if err := keep("register-7", ceremony{userID: 7}); err != nil {
		t.Fatalf("registration blocked: %v", err)
	}
}
