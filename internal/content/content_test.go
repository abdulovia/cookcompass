package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var base = fstest.MapFS{
	"cuisines.json":         {Data: []byte(`[{"slug":"thai","name":"Thai"},{"slug":"greek","name":"Greek"}]`)},
	"recipes/pad-thai.json": {Data: []byte(`{"name":"Pad Thai"}`)},
	"locales/en.json":       {Data: []byte(`{"a":"A"}`)},
	"static/app.css":        {Data: []byte(`body{}`)},
}

func TestLayerMissingDirIsBase(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "nope")} {
		if got := Layer(dir, base); got == nil {
			t.Fatal("nil fs")
		} else if _, layered := got.(*Layered); layered {
			t.Fatalf("%q: expected the base fs unchanged", dir)
		}
	}
}

func TestLayeredOverrideAndUnion(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"recipes/pad-thai.json": `{"name":"My Pad Thai"}`,
		"recipes/mine.json":     `{"name":"Mine"}`,
		"static/app.css":        `body{color:red}`,
		"static/logo.svg":       `<svg/>`,
	})
	fsys := Layer(dir, base)

	if b, _ := fs.ReadFile(fsys, "recipes/pad-thai.json"); !strings.Contains(string(b), "My Pad Thai") {
		t.Fatalf("top should win: %s", b)
	}
	if b, _ := fs.ReadFile(fsys, "cuisines.json"); !strings.Contains(string(b), "greek") {
		t.Fatal("base file should show through")
	}
	got, err := fs.Glob(fsys, "recipes/*.json")
	if err != nil || strings.Join(got, ",") != "recipes/mine.json,recipes/pad-thai.json" {
		t.Fatalf("glob union: %v %v", got, err)
	}
	entries, err := fs.ReadDir(fsys, "static")
	if err != nil || len(entries) != 2 {
		t.Fatalf("readdir union: %v %v", entries, err)
	}
	if _, err := fs.ReadFile(fsys, "missing.json"); err == nil {
		t.Fatal("missing file should error")
	}

	sub, err := Sub(fsys, "static")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(sub, "app.css"); string(b) != "body{color:red}" {
		t.Fatalf("sub override: %s", b)
	}
	if _, err := fs.ReadFile(sub, "logo.svg"); err != nil {
		t.Fatalf("sub top-only file: %v", err)
	}

	layers, err := ReadLayers(fsys, "recipes/pad-thai.json")
	if err != nil || len(layers) != 2 || !strings.Contains(string(layers[1]), "My") {
		t.Fatalf("layers bottom-up: %q %v", layers, err)
	}
	if one, err := ReadLayers(base, "cuisines.json"); err != nil || len(one) != 1 {
		t.Fatalf("plain fs: %d %v", len(one), err)
	}
	if _, err := ReadLayers(fsys, "nope.json"); err == nil {
		t.Fatal("ReadLayers on a missing file should error")
	}
}

func TestMergeJSON(t *testing.T) {
	type cuisine struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	dir := writeFiles(t, map[string]string{
		"cuisines.json": `[{"slug":"greek","name":"Hellenic"},{"slug":"georgian","name":"Georgian"}]`,
	})
	list, err := MergeJSON(Layer(dir, base), "cuisines.json", func(c cuisine) string { return c.Slug })
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range list {
		got = append(got, c.Slug+"="+c.Name)
	}
	if want := "thai=Thai,greek=Hellenic,georgian=Georgian"; strings.Join(got, ",") != want {
		t.Fatalf("merge: %v, want %s", got, want)
	}

	bad := writeFiles(t, map[string]string{"cuisines.json": `{oops`})
	if _, err := MergeJSON(Layer(bad, base), "cuisines.json", func(c cuisine) string { return c.Slug }); err == nil ||
		!strings.Contains(err.Error(), "cuisines.json") {
		t.Fatalf("broken content file should name the file: %v", err)
	}
}
