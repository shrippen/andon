//go:build !release

package seed

import (
	"context"
	"database/sql"
	_ "embed"
	"log/slog"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/accounts"
	"andon/internal/services/analysis"
	"andon/internal/services/connections"
	"andon/internal/services/porting"
	"andon/internal/sources"
)

// Demo accounts from the shared shrippen demo world, Studio Weber (made-up
// data, connections use demo:// URLs, see package sources/demoworld).
const (
	DemoAdmin    = "lena@studio-weber.example.test"
	DemoUser     = "mara@studio-weber.example.test"
	DemoPassword = "demo-password-1"
	demoTeam     = "Produktion"
)

//go:embed demo/instance.yml
var demoInstance string

//go:embed demo/personal.yml
var demoPersonal string

// Demo creates demo users and boards once (only while no user exists).
func Demo(ctx context.Context, d *sql.DB) error {
	var adminID, userID int64
	created := false
	err := db.WithTx(d, func(tx *sql.Tx) error {
		n, err := users.Count(tx)
		if err != nil || n > 0 {
			return err
		}
		password := DemoPassword
		admin, err := accounts.Create(tx, DemoAdmin, "Lena Kraus", &password, enums.RoleAdmin, enums.LocaleDE, "")
		if err != nil {
			return err
		}
		admin.IsBreakglass = true
		if err := users.Update(tx, admin); err != nil {
			return err
		}
		user, err := accounts.Create(tx, DemoUser, "Mara Weber", &password, enums.RoleUser, enums.LocaleDE, "")
		if err != nil {
			return err
		}
		if err := accounts.JoinTeams(tx, user.ID, []accounts.TeamAssignment{{Team: demoTeam, Role: enums.TeamEditor}}); err != nil {
			return err
		}
		if err := accounts.JoinTeams(tx, admin.ID, []accounts.TeamAssignment{{Team: demoTeam, Role: enums.TeamOwner}}); err != nil {
			return err
		}
		adminID, userID, created = admin.ID, user.ID, true
		return nil
	})
	if err != nil || !created {
		return err
	}

	if err := importInto(d, adminID, instanceSpace, demoInstance); err != nil {
		return err
	}
	if err := importInto(d, userID, personalSpace, demoPersonal); err != nil {
		return err
	}
	if err := demoTemplates(d, adminID, userID); err != nil {
		return err
	}
	if err := demoHistory(d, time.Now().UTC()); err != nil {
		return err
	}
	if _, err := analysis.RunAll(ctx, d, time.Now().UTC()); err != nil {
		return err
	}
	slog.Warn("DEMO MODE", "admin", DemoAdmin, "user", DemoUser, "password", DemoPassword)
	return nil
}

// The instance's templates (credentials: personal in instance.yml): the
// user has activated them; the admin then stopped checking the paused
// one's certificate, so that activation waits to be renewed. The admin
// has activated none, so their tiles ask for a login.
var demoActivated = []enums.ServiceType{enums.ServiceNextcloud, enums.ServiceImmich}

const (
	demoPaused = enums.ServiceImmich
	demoLogin  = "demo" // demo:// sources take any login
)

func demoTemplates(d *sql.DB, adminID, userID int64) error {
	admin, err := access.Load(d, adminID)
	if err != nil {
		return err
	}
	user, err := access.Load(d, userID)
	if err != nil {
		return err
	}
	list, err := connections.Listing(d, admin, enums.RightView)
	if err != nil {
		return err
	}
	templates := map[enums.ServiceType]connections.View{}
	for _, c := range list {
		if c.Mode == enums.CredentialPersonal && c.Level == enums.SpaceInstance {
			templates[c.Service] = c
		}
	}

	for _, service := range demoActivated {
		c, ok := templates[service]
		if !ok {
			continue
		}
		if err := connections.Activate(d, user, c.ID, model.UserHolder(userID), demoLogin); err != nil {
			return err
		}
	}
	c, ok := templates[demoPaused]
	if !ok {
		return nil
	}
	return connections.Update(d, admin, c.ID, c.Name, c.URL, c.Mode, nil, connections.TLSSkip, nil)
}

type spaceOf func(q db.Queryer, who *access.Principal) (*model.Space, error)

// demoHistory records what runs of the last weeks would have seen: the
// FRITZ!Box's reconnects, for the ISP report.
func demoHistory(d *sql.DB, now time.Time) error {
	sp, err := content.InstanceSpace(d)
	if err != nil {
		return err
	}
	for _, s := range sources.DemoFritzPast(now) {
		if err := analysis.RecordPast(d, sp.ID, s.At, map[string]any{string(enums.ServiceFritzBox): s.Data}); err != nil {
			return err
		}
	}
	return nil
}

func instanceSpace(q db.Queryer, _ *access.Principal) (*model.Space, error) {
	return content.InstanceSpace(q)
}

func personalSpace(q db.Queryer, who *access.Principal) (*model.Space, error) {
	return content.PersonalSpace(q, who.UserID)
}

func importInto(d *sql.DB, userID int64, target spaceOf, text string) error {
	who, err := access.Load(d, userID)
	if err != nil {
		return err
	}
	space, err := target(d, who)
	if err != nil || space == nil {
		return err
	}
	report, err := porting.ImportSpace(d, who, space.ID, text, porting.Merge)
	if err != nil {
		return err
	}
	if len(report.Skipped) > 0 {
		slog.Warn("demo import skipped", "items", report.Skipped)
	}
	return nil
}
