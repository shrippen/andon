package main

import (
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"andon/internal/services/maintenance"
	"andon/internal/services/porting"
)

const cliUsage = `usage:
  andon                              run the server
  andon healthcheck                  exit 0 if the local server answers /healthz
  andon backup <dir>                 SQLite backup as tar.gz
  andon rotate-key <keyfile>         re-encrypt secrets under a new master key
  andon import --email <e> [--dashy] <file>
                                     import YAML or Dashy conf.yml into a personal space`

// cliCommands are the arguments runCLI handles; anything else starts the server.
var cliCommands = map[string]bool{"backup": true, "rotate-key": true, "import": true, "-h": true, "--help": true, "help": true}

// serving tells whether argv starts the server rather than a command.
func serving(argv []string) bool {
	return len(argv) < 2 || !cliCommands[argv[1]]
}

// runCLI handles the operator subcommands (backup, rotate-key); ok=false
// means argv wasn't one of them, so main should start the server instead.
func runCLI(argv []string, database *sql.DB, dbPath, dataDir string) (ok bool, exitCode int) {
	if len(argv) < 2 {
		return false, 0
	}

	switch argv[1] {
	case "backup":
		if len(argv) != 3 {
			fmt.Fprintln(os.Stderr, cliUsage)
			return true, 2
		}
		archive, err := maintenance.Backup(database, dbPath, argv[2], dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backup:", err)
			return true, 1
		}
		fmt.Println(archive)
		return true, 0

	case "rotate-key":
		if len(argv) != 3 {
			fmt.Fprintln(os.Stderr, cliUsage)
			return true, 2
		}
		raw, err := os.ReadFile(argv[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "rotate-key:", err)
			return true, 1
		}
		count, err := maintenance.RotateKey(database, dbPath, strings.TrimSpace(string(raw)))
		if err != nil {
			fmt.Fprintln(os.Stderr, "rotate-key:", err)
			return true, 1
		}
		fmt.Printf("%d secrets re-encrypted. Replace the master_key secret and restart.\n", count)
		return true, 0

	case "import":
		return true, runImport(argv[2:], database)

	case "-h", "--help", "help":
		fmt.Println(cliUsage)
		return true, 0
	}

	return false, 0
}

// runImport handles "import --email <e> [--dashy] <file>".
func runImport(args []string, database *sql.DB) int {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	email := flags.String("email", "", "owner of the personal space")
	dashy := flags.Bool("dashy", false, "file is a Dashy conf.yml")
	if err := flags.Parse(args); err != nil || *email == "" || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, cliUsage)
		return 2
	}
	text, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		return 1
	}

	kind := porting.KindYAML
	if *dashy {
		kind = porting.KindDashy
	}
	report, err := porting.ImportForEmail(database, *email, string(text), kind)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		return 1
	}
	fmt.Printf("%d boards, %d widgets, %d connections\n", report.Boards, report.Widgets, report.Connections)
	for _, s := range report.Skipped {
		fmt.Println("skipped:", s)
	}
	for _, n := range report.Notes {
		fmt.Println("note:", n)
	}
	return 0
}

// healthTimeout bounds the container healthcheck; Docker kills it at 5 s.
const healthTimeout = 4 * time.Second

// healthcheck asks a running server for /healthz: exit 0 when it answers
// 200, 1 otherwise. baseURL e.g. "http://127.0.0.1:8080".
func healthcheck(baseURL string) int {
	client := &http.Client{Timeout: healthTimeout}
	resp, err := client.Get(baseURL + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: HTTP", resp.StatusCode)
		return 1
	}
	return 0
}
