package detailacts

// Work on a hint from its dialog: a note, a colleague who takes it.

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"andon/internal/services/access"
	"andon/internal/services/hints"
)

func init() {
	register("hints", "work", hintWork)
}

// hintWork assigns the hint when the assignee changed, else adds the
// note; the hints service checks the viewer's right on the hint.
func hintWork(_ context.Context, d *sql.DB, who *access.Principal, c Call) error {
	id, err := strconv.ParseInt(c.Form.Get("hint"), 10, 64)
	if err != nil {
		return ErrBadMark
	}
	note := strings.TrimSpace(c.Form.Get("note"))
	current, err := hints.One(d, who, id)
	if err != nil {
		return err
	}
	if raw := c.Form.Get("assignee"); raw != "" {
		assignee, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return ErrBadMark
		}
		if current.AssigneeID == nil || *current.AssigneeID != assignee {
			return hints.Assign(d, who, id, assignee, note)
		}
	}
	if note == "" {
		return nil
	}
	return hints.AddNote(d, who, id, note)
}
