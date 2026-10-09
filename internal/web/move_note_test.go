package web

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

// TestMovedNoteOffersMove: a test redirected to another server offers
// the move to that address, to whoever may manage the connection.
func TestMovedNoteOffersMove(t *testing.T) {
	msg := "ghostfolio: redirected to https://g.example: use it as the URL"
	result := connections.TestResult{Message: msg, Cause: connections.CauseOf(msg)}

	note := noteOf(result, connections.View{ID: 7, Right: enums.RightManage}, &access.Principal{})
	if note.Text != "conn.cause_moved" || note.FixURL != "/connections/7/move?url=https%3A%2F%2Fg.example" {
		t.Fatalf("manager: %+v", note)
	}
	if note := noteOf(result, connections.View{ID: 7, Right: enums.RightUse}, &access.Principal{}); note.FixURL != "" {
		t.Fatalf("user: %+v", note)
	}
}
