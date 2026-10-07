package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"andon/internal/services/analysis"

	"andon/internal/enums"
	"andon/internal/services/admin"
	"andon/internal/services/oidc"
	"andon/internal/services/selfbackup"
	"andon/internal/services/system"
	"andon/internal/services/themes"
	"andon/internal/settings"
)

const oidcBlankRules = 2

// Settings posts that run something now instead of saving.
const (
	analysisPath = "/admin/settings/analysis"
	backupPath   = "/admin/settings/backup"
)

// RegisterSettingsRoutes wires the admin's instance settings and the
// "reapply authentik groups" action.
func (d Deps) RegisterSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/settings", d.authed(d.handleSettingsPage))
	mux.HandleFunc("GET /admin/operations", d.authed(d.handleOpsPage))
	mux.HandleFunc("POST /admin/settings/general", d.handleSettingsGeneral)
	mux.HandleFunc("POST /admin/settings/network", d.handleSettingsNetwork)
	mux.HandleFunc("POST /admin/settings/server", d.handleSettingsServer)
	mux.HandleFunc("POST /admin/settings/oidc", d.handleSettingsOIDC)
	mux.HandleFunc("POST /admin/settings/oidc/test", d.authed(d.handleSettingsOIDCTest))
	mux.HandleFunc("POST "+analysisPath, d.handleAnalysisRun)
	mux.HandleFunc("POST "+backupPath, d.handleBackupRun)
	mux.HandleFunc("GET /admin/users/{id}/reapply", d.authed(d.handleReapplyPreview))
	mux.HandleFunc("POST /admin/users/{id}/reapply", d.handleReapply)
}

// lines splits a textarea/comma list: "a, b\nc" -> [a b c].
func lines(text string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (d Deps) settingsPage(w http.ResponseWriter, ctx Ctx, status int, extra map[string]any) {
	if !ctx.Who.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	net, err := system.Network(d.DB)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	cfg, err := oidc.Load(d.DB, d.live())
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	themeList, err := themes.Listing(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	registration, err := admin.RegistrationOpen(d.DB)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	defaultTheme, err := themes.DefaultID(d.DB)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	var defaultID int64
	if defaultTheme != nil {
		defaultID = *defaultTheme
	}

	rules := append(append([]oidc.GroupRule{}, cfg.Rules...), make([]oidc.GroupRule, oidcBlankRules)...)
	values := map[string]any{
		"Net": net, "NetModes": []system.NetMode{system.NetOpen, system.NetAllowlist},
		"Networks": strings.Join(net.Networks, "\n"), "Hosts": strings.Join(net.Hosts, "\n"),
		"Iframe":        strings.Join(system.IframeOrigins(d.DB), ", "),
		"MapSource":     system.MapSource(d.DB),
		"ForceTOTP":     system.Flag(d.DB, system.SecurityKey, "force_admin_totp"),
		"Registration":  registration,
		"Location":      system.Flag(d.DB, system.LocationKey, "allowed"),
		"OIDC":          cfg,
		"Rules":         rules,
		"ThemeList":     themeList,
		"DefaultTheme":  defaultID,
		"CallbackURL":   strings.TrimRight(d.Settings.BaseURL, "/") + oidc.CallbackPath,
		"InstanceRoles": []enums.InstanceRole{enums.RoleUser, enums.RoleAdmin},
		"TeamRoles":     []enums.TeamRole{enums.TeamViewer, enums.TeamEditor, enums.TeamOwner},
		"AnalysisEvery": d.live().AnalysisMinutes,
	}
	values["Server"], _ = system.Server(d.DB, ctx.Who, d.Settings)
	values["LogLevels"] = []string{"DEBUG", "INFO", "WARN", "ERROR"}
	if run, ok := analysis.LastRun(); ok {
		values["AnalysisRun"] = run
	}
	if health, err := system.Health(d.DB, ctx.Who); err == nil {
		values["Health"] = health
	}
	if last, err := selfbackup.Last(d.DB); err == nil && last != nil {
		values["Backup"] = last
	}
	values["BackupFiles"], _ = selfbackup.Files(d.Settings.BackupsDir())
	values["BackupKeep"] = selfbackup.Keep
	for k, v := range extra {
		values[k] = v
	}
	page := "admin_settings"
	if ctx.Path == opsPath {
		page = "admin_ops"
	}
	_ = d.Page(w, ctx, page, status, values)
}

func (d Deps) handleSettingsPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.settingsPage(w, ctx, http.StatusOK, nil)
}

// opsPath shows how the instance runs: analysis, jobs, backups.
const opsPath = "/admin/operations"

func (d Deps) handleOpsPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.settingsPage(w, ctx, http.StatusOK, nil)
}

// settingsAction is the shape of every settings post: auth, form, run, back.
func (d Deps) settingsAction(w http.ResponseWriter, r *http.Request, run func(Ctx) error) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	if err := run(ctx); err != nil {
		d.settingsPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	d.flash(w, settingsFlash(r.URL.Path))
	http.Redirect(w, r, backTo(r, "/admin/settings"), http.StatusSeeOther)
}

// settingsFlash is what a settings post says once done: the buttons that
// run something now did it, the others saved.
func settingsFlash(path string) string {
	if path == analysisPath || path == backupPath {
		return flashDone
	}
	return flashSaved
}

// errBadMapSource: the map source is no plain http(s) URL.
var errBadMapSource = errors.New("admin.map_source_bad")

func checked(r *http.Request, name string) bool { return r.FormValue(name) != "" }

func (d Deps) handleSettingsGeneral(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error {
		ip := d.clientIP(r)
		var origins []string
		for _, o := range lines(r.FormValue("iframe")) {
			if system.IsOrigin(o) {
				origins = append(origins, o)
			}
		}
		mapSource := strings.TrimSpace(r.FormValue("map_source"))
		if mapSource != "" && !system.IsMapSource(mapSource) {
			return errBadMapSource
		}
		puts := []struct {
			key   string
			value map[string]any
		}{
			{system.IframeKey, map[string]any{"origins": origins}},
			{system.MapKey, map[string]any{"source": mapSource}},
			{system.SecurityKey, map[string]any{"force_admin_totp": checked(r, "force_admin_totp")}},
			{system.RegistrationKey, map[string]any{"open": checked(r, "registration")}},
			{system.LocationKey, map[string]any{"allowed": checked(r, "location_shared")}},
		}
		for _, p := range puts {
			if err := system.Put(d.DB, ctx.Who, p.key, p.value, ip); err != nil {
				return err
			}
		}

		// Empty = the built-in theme, no instance default.
		raw := r.FormValue("default_theme")
		if raw == "" {
			return themes.SetDefault(d.DB, ctx.Who, nil)
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		return themes.SetDefault(d.DB, ctx.Who, &id)
	})
}

// handleSettingsServer stores the server settings the environment left
// unset.
func (d Deps) handleSettingsServer(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error {
		values := map[string]string{}
		for _, f := range settings.Fields {
			values[f.Env] = r.FormValue(f.Env)
		}
		return system.SetServer(d.DB, ctx.Who, d.Settings, values, d.clientIP(r))
	})
}

// handleBackupRun copies and test-restores the database now.
func (d Deps) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error {
		_, err := selfbackup.RunNow(d.DB, ctx.Who, d.Settings.BackupsDir())
		return err
	})
}

// handleAnalysisRun starts the integration check now.
func (d Deps) handleAnalysisRun(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error { return analysis.RequestRun(ctx.Who) })
}

func (d Deps) handleSettingsNetwork(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error {
		policy := system.NetworkPolicy{
			Mode: system.NetMode(r.FormValue("mode")), Networks: lines(r.FormValue("networks")),
			Hosts: lines(r.FormValue("hosts")), Public: checked(r, "public"),
		}
		return system.SetNetwork(d.DB, ctx.Who, policy, d.clientIP(r))
	})
}

func (d Deps) handleSettingsOIDC(w http.ResponseWriter, r *http.Request) {
	d.settingsAction(w, r, func(ctx Ctx) error {
		groups, roles := r.Form["rule_group"], r.Form["rule_role"]
		teams, teamRoles := r.Form["rule_team"], r.Form["rule_team_role"]
		var rules []oidc.GroupRule
		for i, group := range groups {
			if strings.TrimSpace(group) == "" || i >= len(roles) || i >= len(teams) || i >= len(teamRoles) {
				continue
			}
			rules = append(rules, oidc.GroupRule{Group: group, Role: enums.InstanceRole(roles[i]),
				Team: teams[i], TeamRole: enums.TeamRole(teamRoles[i])})
		}
		cfg := oidc.Config{
			Enabled: checked(r, "enabled"), Issuer: r.FormValue("issuer"), ClientID: r.FormValue("client_id"),
			Label: r.FormValue("label"), Only: checked(r, "only"), AutoCreate: checked(r, "auto_create"),
			EmailLink: checked(r, "email_link"), Rules: rules,
		}
		return oidc.Save(d.DB, ctx.Who, d.Settings, cfg, r.FormValue("secret"), d.clientIP(r))
	})
}

func (d Deps) handleSettingsOIDCTest(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	issuer, err := oidc.Test(r.Context(), d.DB, d.live(), ctx.Who)
	if err != nil {
		d.settingsPage(w, ctx, http.StatusBadGateway, map[string]any{"OIDCTest": err.Error(), "OIDCTestOK": false})
		return
	}
	d.settingsPage(w, ctx, http.StatusOK, map[string]any{"OIDCTest": issuer, "OIDCTestOK": true})
}

func (d Deps) handleReapplyPreview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := adminUserID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	plan, err := oidc.PreviewReapply(d.DB, d.live(), ctx.Who, id)
	if err != nil {
		d.pageError(w, ctx, err)
		return
	}
	_ = d.Page(w, ctx, "admin_reapply", http.StatusOK, map[string]any{"Plan": plan, "UserID": id})
}

func (d Deps) handleReapply(w http.ResponseWriter, r *http.Request) {
	d.adminAction(w, r, func(ctx Ctx, id int64) error {
		return oidc.Reapply(d.DB, d.live(), ctx.Who, id, d.clientIP(r))
	})
}
