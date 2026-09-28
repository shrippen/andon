// Command andon runs the HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// The image has no zoneinfo; clocks and greetings need Europe/Berlin & co.
	_ "time/tzdata"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/services/assist"
	"andon/internal/services/auth"
	"andon/internal/services/icons"
	"andon/internal/services/mail"
	"andon/internal/services/scheduler"
	"andon/internal/services/seed"
	"andon/internal/services/summary"
	"andon/internal/services/system"
	"andon/internal/services/themes"
	"andon/internal/settings"
	"andon/internal/web"
)

const shutdownTimeout = 10 * time.Second

// Server limits: slow clients must not hold connections open (Slowloris).
// Writes get room for the slowest handlers (tax ZIP, LLM advice).
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = time.Minute
	writeTimeout      = 5 * time.Minute
	idleTimeout       = 2 * time.Minute
	maxHeaderBytes    = 64 << 10
)

func main() {
	// Needs neither key nor database: it only asks the running server.
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck("http://127.0.0.1" + listenAddr()))
	}

	cfg := settings.Load()

	masterKey := cfg.MasterKey
	if masterKey == "" {
		slog.Error("MASTER_KEY is required (see docker-compose.example.yml)")
		os.Exit(1)
	}
	crypto.Init(masterKey)

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		slog.Error("create data dir", "err", err)
		os.Exit(1)
	}
	dbKey, err := crypto.DatabaseKey(nil)
	if err != nil {
		slog.Error("database key", "err", err)
		os.Exit(1)
	}
	if serving(os.Args) {
		defer lockDB(cfg.DBPath()).Release()
	}
	database, err := db.Open(cfg.DBPath(), dbKey)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if handled, code := runCLI(os.Args, database, cfg.DBPath(), cfg.DataDir); handled {
		os.Exit(code)
	}

	if _, err := themes.EnsureBuiltin(database); err != nil {
		slog.Error("ensure builtin theme", "err", err)
		os.Exit(1)
	}
	if err := system.Start(database); err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
	mail.Init(cfg)
	summary.Init(cfg)
	assist.Init(cfg)
	themes.InitFonts(cfg.ThemesDir())
	icons.Init(cfg.IconsDir())

	// Demo and seed.yml run before the setup code: the demo creates users.
	if cfg.Demo {
		if err := seed.Demo(context.Background(), database); err != nil {
			slog.Error("demo", "err", err)
		}
	}
	if cfg.SeedFile != "" {
		if err := seed.FromFile(database, cfg.SeedFile); err != nil {
			slog.Error("seed file", "err", err)
		}
	}
	if _, err := auth.EnsureSetupCode(database); err != nil {
		slog.Error("ensure setup code", "err", err)
		os.Exit(1)
	}
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	defer stopScheduler()
	if cfg.SchedulerEnabled {
		scheduler.Start(schedulerCtx, backgroundJobs(database, cfg))
	}

	deps := web.Deps{DB: database, Settings: cfg}
	mux := http.NewServeMux()
	deps.RegisterAuthRoutes(mux)
	deps.RegisterBoardRoutes(mux)
	deps.RegisterThemeRoutes(mux)
	deps.RegisterConnectionRoutes(mux)
	deps.RegisterEditorRoutes(mux)
	deps.RegisterNotifyRoutes(mux)
	deps.RegisterStaticRoutes(mux)
	deps.RegisterHintRoutes(mux)
	deps.RegisterProfileRoutes(mux)
	deps.RegisterSecurityRoutes(mux)
	deps.RegisterTeamRoutes(mux)
	deps.RegisterShareRoutes(mux)
	deps.RegisterAdminRoutes(mux)
	deps.RegisterAccountRoutes(mux)
	deps.RegisterAPIRoutes(mux)
	deps.RegisterSettingsRoutes(mux)
	deps.RegisterOIDCRoutes(mux)
	deps.RegisterIconRoutes(mux)
	deps.RegisterPortingRoutes(mux)
	deps.RegisterSpaceRoutes(mux)
	deps.RegisterMoreRoutes(mux)
	deps.RegisterWelcomeRoutes(mux)
	deps.RegisterPasskeyRoutes(mux)
	deps.RegisterHookRoutes(mux)
	deps.RegisterBillingRoutes(mux)
	deps.RegisterClientRoutes(mux)
	deps.RegisterInsightRoutes(mux)
	deps.RegisterStartPageRoutes(mux)
	deps.RegisterHealthRoute(mux)

	server := &http.Server{
		Addr: listenAddr(), Handler: deps.Secure(mux),
		ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout,
		WriteTimeout: writeTimeout, IdleTimeout: idleTimeout, MaxHeaderBytes: maxHeaderBytes,
	}

	go func() {
		slog.Info("listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = server.Shutdown(ctx)
}

// lockRetry is how often a second server looks whether the first is gone.
const lockRetry = 2 * time.Second

// lockDB waits until no other server uses the database; during a deploy
// the new container starts once the old one has stopped.
func lockDB(path string) *db.Held {
	for logged := false; ; logged = true {
		held, err := db.Lock(path)
		if err == nil {
			return held
		}
		if !errors.Is(err, db.ErrLocked) {
			slog.Error("lock database", "err", err)
			os.Exit(1)
		}
		if !logged {
			slog.Warn("another Andon process uses this database; waiting until it stops", "path", path)
		}
		time.Sleep(lockRetry)
	}
}

// listenAddr is the server's address, ":8080" unless PORT says otherwise.
func listenAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}
