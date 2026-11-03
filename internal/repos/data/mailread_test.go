package data

import (
	"testing"

	"andon/internal/model"
)

// A holder's own row wins; a row from before keys matches by UID for
// everyone; another holder's row does not.
func TestReadOf(t *testing.T) {
	a, b := model.UserHolder(1), model.UserHolder(2)
	rows := []MailRead{
		{Holder: a, UID: 5, Mail: "5 x", Fields: map[string]any{"dismissed": "a"}},
		{UID: 6, Fields: map[string]any{"forwarded": "old"}},
	}
	if ReadOf(rows, a, 5, "5 x")["dismissed"] != "a" {
		t.Fatal("own row not found")
	}
	if ReadOf(rows, b, 5, "5 x") != nil {
		t.Fatal("another holder's row matched")
	}
	if ReadOf(rows, b, 6, "6 y")["forwarded"] != "old" {
		t.Fatal("row from before keys not matched by UID")
	}
	f := ReadOf(rows, a, 5, "5 x")
	f["x"] = 1
	if _, ok := rows[0].Fields["x"]; ok {
		t.Fatal("ReadOf hands out the stored map")
	}
}
