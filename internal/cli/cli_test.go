package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/config"
)

func newTestApp() (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return &App{Stdout: &out, Stderr: &errb, Stdin: strings.NewReader("")}, &out, &errb
}

func TestRunVersionHelpUnknown(t *testing.T) {
	t.Setenv("COOK_CONFIG", "testdata-does-not-exist.yaml")
	tests := []struct {
		args    []string
		code    int
		stdout  string
		stderrs string
	}{
		{[]string{"version"}, 0, "dev", ""},
		{[]string{"help"}, 0, "", "commands:"},
		{[]string{"nope"}, 2, "", "unknown command"},
	}
	for _, tc := range tests {
		app, out, errb := newTestApp()
		if code := app.Run(tc.args); code != tc.code {
			t.Errorf("%v: code %d want %d (stderr %s)", tc.args, code, tc.code, errb)
		}
		if !strings.Contains(out.String(), tc.stdout) || !strings.Contains(errb.String(), tc.stderrs) {
			t.Errorf("%v: stdout=%q stderr=%q", tc.args, out, errb)
		}
	}
}

func TestRunBadConfig(t *testing.T) {
	t.Setenv("COOK_KEEP_REVISIONS", "zero")
	app, _, errb := newTestApp()
	if code := app.Run([]string{"version"}); code != 2 {
		t.Fatalf("code %d, stderr %s", code, errb)
	}
}

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	app, _, errb := newTestApp()
	if code := app.Run([]string{"healthcheck", "--url", ok.URL}); code != 0 {
		t.Fatalf("healthy server reported unhealthy: %s", errb)
	}
	if code := app.Run([]string{"healthcheck", "--url", bad.URL}); code != 1 {
		t.Fatal("unhealthy server reported healthy")
	}
	t.Setenv("COOK_ADDR", "127.0.0.1:1")
	if code := app.Run([]string{"healthcheck"}); code != 1 {
		t.Fatal("closed port reported healthy")
	}
	if code := app.Run([]string{"healthcheck", "--bogus"}); code != 1 {
		t.Fatal("bad flag accepted")
	}
}

func TestNewLogger(t *testing.T) {
	var b bytes.Buffer
	NewLogger(&b, config.Log{Level: "debug", Format: "text"}).Debug("hello")
	if !strings.Contains(b.String(), "hello") {
		t.Fatalf("text logger output %q", b.String())
	}
	b.Reset()
	NewLogger(&b, config.Log{Level: "garbage", Format: "json"}).Info("x")
	if !strings.HasPrefix(b.String(), "{") {
		t.Fatalf("json logger output %q", b.String())
	}
}

func TestServeLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "serve.db")
	go func() { done <- Serve(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), ln) }()
	url := "http://" + ln.Addr().String() + "/healthz"
	var resp *http.Response
	for i := 0; i < 50; i++ {
		if resp, err = http.Get(url); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve returned %v", err)
	}
}

func TestServeBadDB(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DB.Path = "/dev/null/nope/x.db"
	if err := Serve(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), ln); err == nil {
		t.Fatal("expected error")
	}
}

// run executes a CLI command against a temp database.
func run(t *testing.T, dbPath string, stdin string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("COOK_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	t.Setenv("COOK_DB", dbPath)
	t.Setenv("COOK_LOG_LEVEL", "error")
	var out, errb bytes.Buffer
	app := &App{Stdout: &out, Stderr: &errb, Stdin: strings.NewReader(stdin)}
	code := app.Run(args)
	return code, out.String(), errb.String()
}

func TestDataCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cli.db")
	if code, out, errs := run(t, dbPath, "", "migrate"); code != 0 || !strings.Contains(out, "schema version") {
		t.Fatalf("migrate %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, dbPath, "", "seed"); code != 0 || !strings.Contains(out, "recipes_added=") {
		t.Fatalf("seed %d %s %s", code, out, errs)
	}
	code, out, errs := run(t, dbPath, "", "recipe", "create", "--file", "../../docs/examples/recipe.json")
	if code != 0 || !strings.Contains(out, "created recipe") {
		t.Fatalf("recipe create %d %s %s", code, out, errs)
	}
	recipeJSON, _ := os.ReadFile("../../docs/examples/recipe.json")
	if code, out, _ := run(t, dbPath, string(recipeJSON), "recipe", "create", "--file", "-"); code != 0 || !strings.Contains(out, "buttered-toast-with-honey-2") {
		t.Fatalf("recipe create from stdin %d %s", code, out)
	}
	if code, out, _ := run(t, dbPath, "", "recipe", "list"); code != 0 || !strings.Contains(out, "buttered-toast-with-honey") {
		t.Fatalf("recipe list %d %s", code, out)
	}
	if code, out, errs := run(t, dbPath, "", "recipe", "delete", "--slug", "buttered-toast-with-honey"); code != 0 || !strings.Contains(out, "deleted") {
		t.Fatalf("recipe delete %d %s %s", code, out, errs)
	}
	if code, _, errs := run(t, dbPath, "", "recipe", "delete", "--slug", "buttered-toast-with-honey"); code != 1 || !strings.Contains(errs, "already deleted") {
		t.Fatalf("double delete %d %s", code, errs)
	}
	if code, out, _ := run(t, dbPath, "", "recipe", "list", "--deleted"); code != 0 || !strings.Contains(out, "buttered-toast-with-honey") {
		t.Fatalf("recipe list deleted %d %s", code, out)
	}
	if code, out, _ := run(t, dbPath, "", "recipe", "restore", "--slug", "buttered-toast-with-honey"); code != 0 || !strings.Contains(out, "restored") {
		t.Fatalf("recipe restore %d %s", code, out)
	}
	if code, out, _ := run(t, dbPath, "", "approve-all"); code != 0 || !strings.Contains(out, "approved 0 recipes") {
		t.Fatalf("approve-all %d %s", code, out)
	}
	if code, out, _ := run(t, dbPath, "", "purge-pending"); code != 0 || !strings.Contains(out, "purged 0") {
		t.Fatalf("purge-pending %d %s", code, out)
	}
	backup := filepath.Join(t.TempDir(), "b", "snap.db")
	if code, out, errs := run(t, dbPath, "", "backup", "--out", backup); code != 0 || !strings.Contains(out, backup) {
		t.Fatalf("backup %d %s %s", code, out, errs)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}

	failures := [][]string{
		{"recipe"},
		{"recipe", "explode"},
		{"recipe", "create"},
		{"recipe", "create", "--file", "missing.json"},
		{"recipe", "delete"},
		{"recipe", "delete", "--slug", "nope"},
		{"recipe", "create", "--bogus"},
		{"backup", "--out", backup},
		{"backup", "--bogus"},
	}
	for _, args := range failures {
		if code, _, _ := run(t, dbPath, "", args...); code == 0 {
			t.Errorf("%v: expected failure", args)
		}
	}
	if code, _, _ := run(t, dbPath, "{\"unknown\": 1}", "recipe", "create", "--file", "-"); code == 0 {
		t.Error("unknown JSON fields accepted")
	}
	if code, _, _ := run(t, dbPath, `{"cuisine":"x","ingredients":[{"text":"1 egg","ingredient":"nope"}]}`, "recipe", "create", "--file", "-"); code == 0 {
		t.Error("unknown ingredient accepted")
	}
	if code, _, _ := run(t, dbPath, `{"cuisine":"british"}`, "recipe", "create", "--file", "-"); code == 0 {
		t.Error("invalid recipe accepted")
	}
}

func TestCommandsFailWithBadDB(t *testing.T) {
	for _, args := range [][]string{{"migrate"}, {"seed"}, {"approve-all"}, {"purge-pending"}, {"backup"}, {"recipe", "list"},
		{"recipe", "delete", "--id", "1"}, {"recipe", "create", "--file", "../../docs/examples/recipe.json"}} {
		if code, _, _ := run(t, "/dev/null/x/y.db", "", args...); code != 1 {
			t.Errorf("%v: expected exit 1, got %d", args, code)
		}
	}
}

func TestScrapeCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "scrape.db")
	fixture, _ := filepath.Abs("../ingest/testdata/graph_recipe.html")
	t.Setenv("COOK_SCRAPE_TARGETS", "../../seed/sources/jsonld_targets.txt")
	t.Setenv("COOK_SCRAPE_CACHE", t.TempDir())
	code, out, errs := run(t, dbPath, "", "scrape", "--source", "jsonld", "--url", "file:"+fixture)
	if code != 0 || !strings.Contains(out, "created=1") || !strings.Contains(out, "review new items") {
		t.Fatalf("scrape url: %d %s %s", code, out, errs)
	}
	code, out, _ = run(t, dbPath, "", "scrape", "--limit", "2")
	if code != 0 || !strings.Contains(out, "fetched=2 created=2") {
		t.Fatalf("scrape bundled samples: %d %s", code, out)
	}
	// Offline / unreachable: warn and keep going.
	code, out, _ = run(t, dbPath, "", "scrape", "--url", "http://127.0.0.1:1/offline")
	if code != 0 || !strings.Contains(out, "fetched=0") {
		t.Fatalf("offline scrape should not fail: %d %s", code, out)
	}
	if code, out, _ := run(t, dbPath, "", "approve-all"); code != 0 || !strings.Contains(out, "approved 3 recipes") {
		t.Fatalf("approve-all after scrape: %s", out)
	}
	if code, _, _ := run(t, dbPath, "", "scrape", "--source", "nope"); code != 1 {
		t.Fatal("unknown source accepted")
	}
	if code, _, _ := run(t, dbPath, "", "scrape", "--bogus"); code == 0 {
		t.Fatal("bad flag accepted")
	}
	if code, _, _ := run(t, "/dev/null/x.db", "", "scrape"); code != 1 {
		t.Fatal("bad db accepted")
	}
}
