package caps

import (
	"testing"

	"andon/internal/enums"
)

// Every member names its category, and no service is in two.
func TestCategoryMembers(t *testing.T) {
	seen := map[enums.ServiceType]Category{}
	for _, c := range AllCategories() {
		for _, s := range Members(c) {
			if other, ok := seen[s]; ok {
				t.Errorf("%s in %s and %s", s, other, c)
			}
			seen[s] = c
			if got, ok := CategoryOf(s); !ok || got != c {
				t.Errorf("CategoryOf(%s) = %q, want %q", s, got, c)
			}
		}
	}
	if _, ok := CategoryOf(enums.ServiceKimai); ok {
		t.Error("Kimai has no category")
	}
	if got := Members(Bank); len(got) < 2 || got[0] != enums.ServiceSure {
		t.Errorf("bank members = %v, want Sure first", got)
	}
}
