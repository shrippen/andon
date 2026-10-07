package widgets

import "testing"

// Table rows carry ints as well as floats (overdue days): the dialog's
// cell said "–" for 21 days overdue because only float64 counted.
func TestTableCellTakesInts(t *testing.T) {
	if c := tableCell("late", 21); c.Value == "–" {
		t.Fatalf("late 21: %+v", c)
	}
	if asF(int64(3)) != 3 || asF(int(2)) != 2 || asF(float32(1.5)) != 1.5 {
		t.Fatal("asF misses a number type")
	}
}
