package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesEnvAndIsIdempotent(t *testing.T) {
	t.Setenv("COOK_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	dir := t.TempDir()
	example := "PORT=9090\nCOOK_ADMIN_USER=chef\nCOOK_ADMIN_PASSWORD=change-me\n# MINIO_ROOT_PASSWORD=commented\nMINIO_ROOT_PASSWORD=cookcompass-secret\nOTHER=keep\n"
	if err := os.WriteFile(filepath.Join(dir, ".env.example"), []byte(example), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, errb := newTestApp()
	if code := app.Run([]string{"init", "--dir", dir}); code != 0 {
		t.Fatalf("init: %d %s", code, errb)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnv(string(b))
	for _, k := range secretKeys {
		if exampleSecrets[env[k]] || len(env[k]) < 20 {
			t.Errorf("%s not generated: %q", k, env[k])
		}
	}
	if env["OTHER"] != "keep" || env["PORT"] != "9090" || !strings.Contains(string(b), "# MINIO_ROOT_PASSWORD=commented") {
		t.Fatalf("other lines must be kept: %s", b)
	}
	if st, _ := os.Stat(filepath.Join(dir, ".env")); st.Mode().Perm() != 0o600 {
		t.Errorf(".env should be private: %v", st.Mode())
	}
	if st, err := os.Stat(filepath.Join(dir, "content")); err != nil || !st.IsDir() {
		t.Fatal("content/ not created")
	}
	if !strings.Contains(out.String(), "http://localhost:9090/admin") || !strings.Contains(out.String(), "user chef, password "+env["COOK_ADMIN_PASSWORD"]) {
		t.Fatalf("summary: %s", out)
	}

	// A second run changes nothing.
	app, out, _ = newTestApp()
	if code := app.Run([]string{"init", "--dir", dir}); code != 0 || !strings.Contains(out.String(), "nothing changed") {
		t.Fatalf("second init: %d %s", code, out)
	}
	if again, _ := os.ReadFile(filepath.Join(dir, ".env")); string(again) != string(b) {
		t.Fatal("idempotent init rewrote .env")
	}

	// An existing .env with an example password gets just that fixed, and a
	// missing secret key is appended.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("COOK_ADMIN_PASSWORD=cookcompass\nPORT=1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, out, _ = newTestApp()
	if code := app.Run([]string{"init", "--dir", dir}); code != 0 || !strings.Contains(out.String(), "replaced example values") {
		t.Fatalf("fix init: %d %s", code, out)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ".env"))
	if env = parseEnv(string(b)); env["COOK_ADMIN_PASSWORD"] == "cookcompass" || env["MINIO_ROOT_PASSWORD"] == "" || env["PORT"] != "1234" {
		t.Fatalf("fixed env: %s", b)
	}
}

func TestInitWithoutExample(t *testing.T) {
	t.Setenv("COOK_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	dir := t.TempDir()
	app, out, errb := newTestApp()
	if code := app.Run([]string{"init", "--dir", dir}); code != 0 {
		t.Fatalf("init: %d %s", code, errb)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if env := parseEnv(string(b)); env["COOK_ADMIN_PASSWORD"] == "" || env["PORT"] != "8080" || !strings.Contains(out.String(), "localhost:8080") {
		t.Fatalf("fallback env: %s / %s", b, out)
	}
}

func TestDoctor(t *testing.T) {
	data := t.TempDir()
	t.Setenv("COOK_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
	t.Setenv("COOK_DB", filepath.Join(data, "db.sqlite"))
	t.Setenv("COOK_SCRAPE_CACHE", filepath.Join(data, "cache"))
	t.Setenv("COOK_CATALOG_INBOX", filepath.Join(data, "ci"))
	t.Setenv("COOK_RECIPE_INBOX", filepath.Join(data, "ri"))
	t.Setenv("COOK_LOG_LEVEL", "error")

	// Fresh database, nothing seeded yet: doctor fails and says why.
	app, out, _ := newTestApp()
	if code := app.Run([]string{"doctor"}); code != 1 {
		t.Fatalf("doctor on an empty db: %d\n%s", code, out)
	}
	if !strings.Contains(out.String(), "✗ Recipes published") || !strings.Contains(out.String(), "→ Run `make seed`") {
		t.Fatalf("doctor output: %s", out)
	}

	// Seeded and with an example password: the password is flagged.
	app, _, _ = newTestApp()
	if code := app.Run([]string{"seed"}); code != 0 {
		t.Fatal("seed failed")
	}
	t.Setenv("COOK_ADMIN_PASSWORD", "cookcompass")
	app, out, _ = newTestApp()
	if code := app.Run([]string{"doctor"}); code != 1 || !strings.Contains(out.String(), "✗ Admin password") {
		t.Fatalf("example password should fail: %d\n%s", code, out)
	}

	t.Setenv("COOK_ADMIN_PASSWORD", "a-real-long-secret-value")
	app, out, _ = newTestApp()
	if code := app.Run([]string{"doctor"}); code != 0 {
		t.Fatalf("doctor should pass: %d\n%s", code, out)
	}
	for _, want := range []string{"✓ Database", "✓ Recipes published", "○ Your own recipes (optional)", "All required checks passed"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}
}
