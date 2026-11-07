package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
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
	go func() { done <- Serve(ctx, config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)), ln) }()
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
