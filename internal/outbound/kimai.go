package outbound

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// KimaiStart starts a timer for project and activity (Kimai sets "now").
func KimaiStart(ctx context.Context, to Target, projectID, activityID int64, description string) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	body := map[string]any{"project": projectID, "activity": activityID}
	if description != "" {
		body["description"] = description
	}
	_, err := api.Send(ctx, http.MethodPost, "timesheets", body)
	return err
}

// KimaiSheet is a timesheet write. Begin and End are local times
// ("2026-09-26T09:05:00"), as Kimai reads them in the user's timezone.
// Empty ids and times are left out; Description and Tags are always sent,
// so an edit passes the sheet's current values.
type KimaiSheet struct {
	Project, Activity int64
	Begin, End        string
	Description       string
	Tags              []string
	Billable          enums.Billable
}

// body is the sheet as Kimai's form reads it: tags as one
// comma-separated string, not a list.
func (s KimaiSheet) body() map[string]any {
	out := map[string]any{"description": s.Description, "tags": strings.Join(s.Tags, ",")}
	for key, v := range map[string]int64{"project": s.Project, "activity": s.Activity} {
		if v > 0 {
			out[key] = v
		}
	}
	for key, v := range map[string]string{"begin": s.Begin, "end": s.End} {
		if v != "" {
			out[key] = v
		}
	}
	if s.Billable != enums.BillableDefault {
		out["billable"] = s.Billable == enums.BillableYes
	}
	return out
}

// KimaiCreate books a timesheet (finished if End is set).
func KimaiCreate(ctx context.Context, to Target, sheet KimaiSheet) error {
	return kimaiWrite(ctx, to, http.MethodPost, "timesheets", sheet.body())
}

// KimaiTags creates tags so a timesheet write can use them: Kimai's API
// drops unknown tag names without error. A tag that exists already is
// refused (HTTP 400) and skipped; so is one the user may not create.
func KimaiTags(ctx context.Context, to Target, names []string) {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	for _, name := range names {
		api.Send(ctx, http.MethodPost, "tags", map[string]any{"name": name, "visible": true})
	}
}

// KimaiEdit changes one timesheet; a running one keeps running unless
// End is set.
func KimaiEdit(ctx context.Context, to Target, timesheetID int64, sheet KimaiSheet) error {
	return kimaiWrite(ctx, to, http.MethodPatch, "timesheets/"+strconv.FormatInt(timesheetID, 10), sheet.body())
}

// KimaiDelete removes one timesheet.
func KimaiDelete(ctx context.Context, to Target, timesheetID int64) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodDelete, "timesheets/"+strconv.FormatInt(timesheetID, 10), nil)
	return err
}

// kimaiWrite sends a timesheet. Kimai rejects the billable field without
// the edit_billable permission ("extra fields", HTTP 400), so a rejected
// write is tried once more without it.
func kimaiWrite(ctx context.Context, to Target, method, path string, body map[string]any) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, method, path, body)
	if _, sent := body["billable"]; err == nil || !sent || !strings.Contains(err.Error(), badRequest) {
		return err
	}
	delete(body, "billable")
	_, err = api.Send(ctx, method, path, body)
	return err
}

// badRequest is how the driver reports a rejected form.
const badRequest = "HTTP 400"

// KimaiDescribe sets a timesheet's description.
func KimaiDescribe(ctx context.Context, to Target, timesheetID int64, description string) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "timesheets/"+strconv.FormatInt(timesheetID, 10), map[string]any{"description": description})
	return err
}

// KimaiStop stops one running timesheet.
func KimaiStop(ctx context.Context, to Target, timesheetID int64) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "timesheets/"+strconv.FormatInt(timesheetID, 10)+"/stop", nil)
	return err
}

// KimaiMarkExported flips a timesheet's export flag; call it only for
// sheets that are not exported yet.
func KimaiMarkExported(ctx context.Context, to Target, timesheetID int64) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "timesheets/"+strconv.FormatInt(timesheetID, 10)+"/export", nil)
	return err
}

// KimaiRenameCustomer sets a customer's name (PATCH /api/customers/{id}),
// e.g. to the name Invoice Ninja holds.
func KimaiRenameCustomer(ctx context.Context, to Target, customerID int64, name string) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "customers/"+strconv.FormatInt(customerID, 10), map[string]any{"name": name})
	return err
}
