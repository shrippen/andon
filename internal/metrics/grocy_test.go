package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestGrocyInfoSoon: the link tile counts what is due within Grocy's
// 5 days, though the dataset reaches further.
func TestGrocyInfoSoon(t *testing.T) {
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format(time.DateOnly) }
	data := &sources.GrocyDataset{Soon: []sources.Product{{Name: "Milch", Due: day(2)}, {Name: "Reis", Due: day(20)}}}
	parts := GrocyInfo(data)
	if len(parts) != 1 || parts[0].Params["count"] != 1 {
		t.Fatalf("parts: %+v", parts)
	}
}
