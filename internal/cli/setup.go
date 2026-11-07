package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/abdulovia/cookcompass/internal/app"
	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/i18n"
)

// secretKeys are .env entries init fills with random values when they are
// empty or still hold an example value.
var secretKeys = []string{"COOK_ADMIN_PASSWORD", "MINIO_ROOT_PASSWORD"}

var exampleSecrets = map[string]bool{"": true, "change-me": true, "cookcompass": true, "cookcompass-secret": true}

// fallbackEnv is used when .env.example is missing (e.g. a copied binary).
const fallbackEnv = `PORT=8080
COOK_ADMIN_USER=admin
COOK_ADMIN_PASSWORD=
COOK_DEFAULT_COUNTRY=US
MINIO_ROOT_USER=cookcompass
MINIO_ROOT_PASSWORD=
`

// runInit prepares a checkout for its first start: .env with fresh secrets
// and the content/ folder. It is idempotent: existing values are kept, only
// empty or example secrets are replaced.
func runInit(e *env, args []string) error {
	fl := flag.NewFlagSet("init", flag.ContinueOnError)
	fl.SetOutput(e.Stderr)
	dir := fl.String("dir", ".", "project directory")
	if err := fl.Parse(args); err != nil {
		return err
	}
	envPath := filepath.Join(*dir, ".env")
	src, created := "", false
	if b, err := os.ReadFile(envPath); err == nil {
		src = string(b)
	} else if errors.Is(err, fs.ErrNotExist) {
		created = true
		if b, err := os.ReadFile(filepath.Join(*dir, ".env.example")); err == nil {
			src = string(b)
		} else {
			src = fallbackEnv
		}
	} else {
		return err
	}
	out, generated, err := fillSecrets(src)
	if err != nil {
		return err
	}
	if created || len(generated) > 0 {
		if err := os.WriteFile(envPath, []byte(out), 0o600); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(*dir, "content"), 0o755); err != nil {
		return err
	}
	vals := parseEnv(out)
	port := firstNonEmpty(vals["PORT"], "8080")
	switch {
	case created:
		fmt.Fprintf(e.Stdout, "Created %s with fresh secrets.\n", envPath)
	case len(generated) > 0:
		fmt.Fprintf(e.Stdout, "Updated %s: replaced example values of %s.\n", envPath, strings.Join(generated, ", "))
	default:
		fmt.Fprintf(e.Stdout, "%s is already set up; nothing changed.\n", envPath)
	}
	fmt.Fprintf(e.Stdout, `
Next:  make up            (or: make up-release to use the prebuilt image)
Open:  http://localhost:%s
Admin: http://localhost:%s/admin   user %s, password %s
       (stored in .env — keep it private)
`, port, port, firstNonEmpty(vals["COOK_ADMIN_USER"], "admin"), vals["COOK_ADMIN_PASSWORD"])
	return nil
}

// fillSecrets replaces empty/example secret values, adding missing keys.
func fillSecrets(src string) (string, []string, error) {
	var lines []string
	seen := map[string]bool{}
	var generated []string
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		line := sc.Text()
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if ok && !strings.HasPrefix(key, "#") && isSecret(key) {
			seen[key] = true
			if exampleSecrets[strings.Trim(strings.TrimSpace(val), `"'`)] {
				s, err := randomSecret()
				if err != nil {
					return "", nil, err
				}
				line = key + "=" + s
				generated = append(generated, key)
			}
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return "", nil, err
	}
	for _, k := range secretKeys {
		if !seen[k] {
			s, err := randomSecret()
			if err != nil {
				return "", nil, err
			}
			lines = append(lines, k+"="+s)
			generated = append(generated, k)
		}
	}
	return strings.Join(lines, "\n") + "\n", generated, nil
}

func isSecret(key string) bool {
	for _, k := range secretKeys {
		if k == key {
			return true
		}
	}
	return false
}

func randomSecret() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseEnv(src string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(src, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(strings.TrimSpace(k), "#") {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// runDoctor prints the setup checklist and fails when a required item is
// missing. It opens (and migrates) the database like every command.
func runDoctor(e *env, _ []string) error {
	// The report is the output; only errors from opening the app go to the log.
	e.log = NewLogger(e.Stderr, config.Log{Level: "error", Format: e.cfg.Log.Format})
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	bundle, err := i18n.Load(a.WebFS(), "locales")
	if err != nil {
		return err
	}
	t := bundle.Localizer("en").T
	checks := a.Checks(e.ctx)
	fmt.Fprintln(e.Stdout, "CookCompass doctor")
	open := 0
	for _, c := range checks {
		mark := "✓"
		switch {
		case !c.OK && c.Optional:
			mark, open = "○", open+1
		case !c.OK:
			mark = "✗"
		}
		label := t("check." + c.ID)
		if c.Optional {
			label += " (optional)"
		}
		fmt.Fprintf(e.Stdout, "  %s %-34s %s\n", mark, label, c.Detail)
		if !c.OK {
			fmt.Fprintf(e.Stdout, "      → %s\n", t("check."+c.ID+"_fix"))
		}
	}
	if !app.ChecksPassed(checks) {
		return errors.New("some required checks failed (see ✗ above)")
	}
	fmt.Fprintf(e.Stdout, "All required checks passed; %d optional item(s) open.\n", open)
	return nil
}
