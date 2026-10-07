package web

import (
	"andon/internal/services/oidc"
	"andon/internal/services/util"
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/admin"
	"andon/internal/services/audit"
	"andon/internal/services/invites"
	"andon/internal/services/teams"
)

// RegisterAdminRoutes wires the admin-only account pages: user list with
// role/active/break-glass/reset/delete, invitations, and the audit log.
func (d Deps) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/users", d.authed(d.handleAdminUsers))
	mux.HandleFunc("POST /admin/users/{id}/role", d.handleAdminUserRole)
	mux.HandleFunc("POST /admin/users/{id}/active", d.handleAdminSwitch(admin.SetActive))
	mux.HandleFunc("POST /admin/users/{id}/breakglass", d.handleAdminSwitch(admin.SetBreakglass))
	mux.HandleFunc("POST /admin/users/{id}/reset", d.authed(d.handleAdminUserReset))
	mux.HandleFunc("POST /admin/users/{id}/delete", d.handleAdminUserDelete)
	mux.HandleFunc("POST /admin/invite", d.authed(d.handleAdminInvite))
	mux.HandleFunc("GET /admin/invite", formPage("/admin/users"))
	mux.HandleFunc("POST /admin/invites/{id}/delete", d.handleAdminInviteDelete)
	mux.HandleFunc("GET /admin/audit", d.authed(d.handleAdminAudit))
}

func (d Deps) adminUsersPage(w http.ResponseWriter, ctx Ctx, status int, extra map[string]any) {
	rows, err := admin.Users(d.DB, ctx.Who)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}
	pending, err := invites.Pending(d.DB, ctx.Who)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}
	teamList, err := teams.Overview(d.DB, ctx.Who)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}

	values := map[string]any{"Users": rows, "Invites": pending, "TeamList": teamList}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "admin_users", status, values)
}

// pageError answers a failed admin read: 403 for denial, 500 otherwise.
func (d Deps) pageError(w http.ResponseWriter, ctx Ctx, err error) {
	if errors.Is(err, admin.ErrDenied) || errors.Is(err, invites.ErrDenied) || errors.Is(err, audit.ErrDenied) || errors.Is(err, oidc.ErrDenied) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(err, util.ErrNotFound) {
		http.NotFound(w, nil)
		return
	}
	d.fail(w, err, http.StatusInternalServerError)
}

func (d Deps) handleAdminUsers(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.adminUsersPage(w, ctx, http.StatusOK, nil)
}

func adminUserID(r *http.Request) (int64, error) {
	return pathID(r, "id")
}

// adminAction is the common shape of an admin form post: auth, id, run, back to list.
func (d Deps) adminAction(w http.ResponseWriter, r *http.Request, run func(Ctx, int64) error) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := adminUserID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := run(ctx, id); err != nil {
		d.adminUsersPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (d Deps) handleAdminUserRole(w http.ResponseWriter, r *http.Request) {
	d.adminAction(w, r, func(ctx Ctx, id int64) error {
		return admin.SetRole(d.DB, ctx.Who, id, enums.InstanceRole(r.FormValue("role")), d.clientIP(r))
	})
}

type switchFunc func(d *sql.DB, who *access.Principal, userID int64, state admin.Switch, ip string) error

func (d Deps) handleAdminSwitch(set switchFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.adminAction(w, r, func(ctx Ctx, id int64) error {
			return set(d.DB, ctx.Who, id, admin.Switch(r.FormValue("state")), d.clientIP(r))
		})
	}
}

func (d Deps) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	d.adminAction(w, r, func(ctx Ctx, id int64) error {
		return admin.Delete(d.DB, ctx.Who, id, d.clientIP(r))
	})
}

func (d Deps) handleAdminUserReset(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := adminUserID(r)
	if err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	link, err := invites.AdminResetLink(d.DB, ctx.Who, id)
	if err != nil {
		d.adminUsersPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.adminUsersPage(w, ctx, http.StatusOK, map[string]any{"ResetLink": link})
}

func (d Deps) handleAdminInvite(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}

	teamRole := enums.TeamRole(r.FormValue("team_role"))
	var joins []model.InviteTeam
	for _, name := range r.Form["teams"] {
		if name != "" {
			joins = append(joins, model.InviteTeam{Team: name, Role: teamRole})
		}
	}

	link, err := invites.Create(d.DB, ctx.Who, r.FormValue("email"), enums.InstanceRole(r.FormValue("role")),
		joins, enums.Locale(r.FormValue("locale")))
	if err != nil {
		d.adminUsersPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	sent := invites.SendMail(strings.TrimSpace(r.FormValue("email")), link, ctx.Who.Name, enums.Locale(r.FormValue("locale")))
	d.adminUsersPage(w, ctx, http.StatusOK, map[string]any{"InviteLink": link, "InviteMail": string(sent)})
}

func (d Deps) handleAdminInviteDelete(w http.ResponseWriter, r *http.Request) {
	d.adminAction(w, r, func(ctx Ctx, id int64) error {
		return invites.Revoke(d.DB, ctx.Who, id)
	})
}

// auditRow is an audit entry with its user's name ("" = system) and its
// detail as lines, e.g. "open: false → true".
type auditRow struct {
	*model.AuditEntry
	Who     string
	Details []string
}

// auditValueMax cuts long values (lists, nested settings) in the table.
const auditValueMax = 60

// auditLines formats an entry's detail; a [old, new] pair is a change.
func auditLines(detail map[string]any) []string {
	keys := slices.Sorted(maps.Keys(detail))
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		pair, ok := detail[k].([]any)
		if ok && len(pair) == 2 {
			lines = append(lines, k+": "+auditValue(pair[0])+" → "+auditValue(pair[1]))
			continue
		}
		lines = append(lines, k+": "+auditValue(detail[k]))
	}
	return lines
}

func auditValue(v any) string {
	if v == nil {
		return "–"
	}
	r := []rune(fmt.Sprint(v))
	if len(r) > auditValueMax {
		return string(r[:auditValueMax]) + "…"
	}
	return string(r)
}

func (d Deps) handleAdminAudit(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	entries, err := audit.Entries(d.DB, ctx.Who)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}
	users, err := admin.Users(d.DB, ctx.Who)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}
	names := map[int64]string{}
	for _, u := range users {
		names[u.ID] = u.Name
	}
	rows := make([]auditRow, 0, len(entries))
	for _, e := range entries {
		row := auditRow{AuditEntry: e, Details: auditLines(e.Detail)}
		if e.UserID != nil {
			row.Who = cmp.Or(names[*e.UserID], "#"+strconv.FormatInt(*e.UserID, 10))
		}
		rows = append(rows, row)
	}
	_ = d.Page(w, ctx, "admin_audit", http.StatusOK, map[string]any{"Entries": rows})
}
