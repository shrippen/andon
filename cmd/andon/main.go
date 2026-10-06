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
	"andon/internal/logbuf"
	"andon/internal/outbound"
	"andon/internal/services/auth"
	"andon/internal/services/icons"
	"andon/internal/services/maintenance"
	"andon/internal/services/scheduler"
	"andon/internal/services/seed"
	"andon/internal/services/svcdata"
	"andon/internal/services/system"
	"andon/internal/services/themes"
	"andon/internal/services/widgetlib"
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
	os.Exit(run())
}

// run serves until a stop signal and returns the exit code; its defers
// (scheduler, database, lock) run before the process exits.
func run() int {
	// Needs neither key nor database: it only asks the running server.
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck("http://127.0.0.1" + listenAddr()))
	}

	// Recent records also go to Admin → Operations (logbuf); the level
	// follows LOG_LEVEL or the UI's server settings.
	slog.SetDefault(slog.New(logbuf.Wrap(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: settings.Level}))))

	cfg := settings.Load()
	settings.Publish(cfg)

	masterKey := cfg.MasterKey
	if masterKey == "" {
		slog.Error("MASTER_KEY is required (see docker-compose.example.yml)")
		os.Exit(1)
	}
	// A guessable key would give a stolen database away; the server
	// refuses it, rotate-key (a command) still runs to replace it.
	if crypto.WeakKey(masterKey) && serving(os.Args) && !cfg.Dev {
		slog.Error("MASTER_KEY is guessable; replace it: andon rotate-key <file with the output of openssl rand -base64 32>")
		os.Exit(1)
	}
	restrictFiles()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		slog.Error("create data dir", "err", err)
		os.Exit(1)
	}
	if serving(os.Args) {
		defer lockDB(cfg.DBPath()).Release()
	}
	database, unlocked, err := maintenance.Unlock(cfg.DBPath(), masterKey)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if unlocked == maintenance.UnlockedUpgraded {
		slog.Warn("database re-encrypted under an Argon2id key; keep andon.db.salt with your backups, webhook URLs changed")
	}

	if handled, code := runCLI(os.Args, database, cfg.DBPath(), cfg.DataDir); handled {
		os.Exit(code)
	}

	if _, err := themes.EnsureBuiltin(database); err != nil {
		slog.Error("ensure builtin theme", "err", err)
		os.Exit(1)
	}
	// Stored tile configs under renamed keys move to the current ones.
	upgraded, err := widgetlib.UpgradeConfigs(database)
	if err != nil {
		slog.Error("upgrade widget configs", "err", err)
		os.Exit(1)
	}
	if upgraded > 0 {
		slog.Info("upgraded widget configs", "count", upgraded)
	}
	if err := system.Start(database); err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
	// Server settings saved in the UI fill what the environment left unset.
	if err := system.ApplyServer(database, cfg); err != nil {
		slog.Error("server settings", "err", err)
		return 1
	}
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
	waitJobs := func() {}
	if cfg.SchedulerEnabled {
		waitJobs = scheduler.Start(schedulerCtx, backgroundJobs(database, cfg))
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
	deps.RegisterSiteRoutes(mux)
	deps.RegisterMoreRoutes(mux)
	deps.RegisterWelcomeRoutes(mux)
	deps.RegisterAboutRoutes(mux)
	deps.RegisterPasskeyRoutes(mux)
	deps.RegisterHookRoutes(mux)
	deps.RegisterBillingRoutes(mux)
	deps.RegisterClientRoutes(mux)
	deps.RegisterHostRoutes(mux)
	deps.RegisterInsightRoutes(mux)
	deps.RegisterStartPageRoutes(mux)
	deps.RegisterHealthRoute(mux)

	server := &http.Server{
		Addr: listenAddr(), Handler: deps.Secure(mux),
		ReadHeaderTimeout: readHeaderTimeout, ReadTimeout: readTimeout,
		WriteTimeout: writeTimeout, IdleTimeout: idleTimeout, MaxHeaderBytes: maxHeaderBytes,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", server.Addr)
		serveErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	failed := false
	select {
	case <-stop:
	case err := <-serveErr:
		slog.Error("serve", "err", err)
		failed = true
	}

	// Stop taking requests, then let jobs, fills and mails finish before
	// the deferred database close; all within the shutdown timeout.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = server.Shutdown(ctx)
	stopScheduler()
	drain(ctx, waitJobs, svcdata.WaitFills, outbound.WaitSent, icons.Wait)
	if failed {
		return 1
	}
	return 0
}

// drain runs the waits one after another until ctx ends.
func drain(ctx context.Context, waits ...func()) {
	done := make(chan struct{})
	go func() {
		for _, wait := range waits {
			wait()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("shutdown: background work still running")
	}
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
