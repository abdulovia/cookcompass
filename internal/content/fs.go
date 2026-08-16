// Package content layers an operator-editable directory over the embedded
// defaults. The embedded seed and UI files keep the app runnable offline and
// out of the box; anything placed in the content directory (COOK_CONTENT_DIR)
// adds to or overrides them without a rebuild:
//
//	content/
//	  site.yaml                       branding and feature switches
//	  cuisines.json countries.json    reference data, merged entry by entry
//	  categories.json ingredients.json
//	  shops.csv products/<cc>.csv     shop presets and catalogs
//	  recipes/<slug>.json             extra recipes (same format as seed/)
//	  locales/<lang>.json             UI text overrides, merged key by key
//	  static/...                      extra or replaced static assets
//
// Code holds behaviour; content holds words and data.
package content

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"sort"
)

// Layered serves names from Top when present and from Base otherwise.
// Directory listings and globs return the union of both, Top winning on
// equal names. Layers() exposes both for callers that merge file contents
// (JSON lists by key, locale messages by id) instead of replacing them.
type Layered struct {
	Top  fs.FS // operator content; may be nil
	Base fs.FS // embedded defaults
}

// Layer returns base overlaid with the directory dir. An empty or missing
// dir yields base unchanged, so callers never need to special-case it.
func Layer(dir string, base fs.FS) fs.FS {
	if dir == "" {
		return base
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return base
	}
	return &Layered{Top: os.DirFS(dir), Base: base}
}

// Sub returns the layered view of a subdirectory, e.g. "static".
func Sub(fsys fs.FS, dir string) (fs.FS, error) {
	if l, ok := fsys.(*Layered); ok {
		base, err := fs.Sub(l.Base, dir)
		if err != nil {
			return nil, err
		}
		top, err := fs.Sub(l.Top, dir)
		if err != nil {
			return nil, err
		}
		return &Layered{Top: top, Base: base}, nil
	}
	return fs.Sub(fsys, dir)
}

// Open implements fs.FS.
func (l *Layered) Open(name string) (fs.File, error) {
	if l.Top != nil {
		f, err := l.Top.Open(name)
		if err == nil {
			if st, serr := f.Stat(); serr == nil && !st.IsDir() {
				return f, nil
			}
			_ = f.Close()
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrInvalid) {
			return nil, err
		}
	}
	return l.Base.Open(name)
}

// ReadFile implements fs.ReadFileFS.
func (l *Layered) ReadFile(name string) ([]byte, error) {
	if l.Top != nil {
		if b, err := fs.ReadFile(l.Top, name); err == nil {
			return b, nil
		}
	}
	return fs.ReadFile(l.Base, name)
}

// ReadDir implements fs.ReadDirFS: the union of both layers.
func (l *Layered) ReadDir(name string) ([]fs.DirEntry, error) {
	byName := map[string]fs.DirEntry{}
	base, berr := fs.ReadDir(l.Base, name)
	for _, e := range base {
		byName[e.Name()] = e
	}
	var terr error = fs.ErrNotExist
	if l.Top != nil {
		var top []fs.DirEntry
		top, terr = fs.ReadDir(l.Top, name)
		for _, e := range top {
			byName[e.Name()] = e
		}
	}
	if berr != nil && terr != nil {
		return nil, berr
	}
	out := make([]fs.DirEntry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Glob implements fs.GlobFS: the union of both layers, sorted.
func (l *Layered) Glob(pattern string) ([]string, error) {
	seen := map[string]bool{}
	for _, layer := range l.Layers() {
		matches, err := fs.Glob(layer, pattern)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			seen[m] = true
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// Layers returns the layers bottom-up: Base first, then Top when set.
func (l *Layered) Layers() []fs.FS {
	if l.Top == nil {
		return []fs.FS{l.Base}
	}
	return []fs.FS{l.Base, l.Top}
}

// ReadLayers returns every layer's copy of name, bottom-up. Plain file
// systems yield at most one copy. Missing copies are skipped; an error is
// returned only when no layer has the file.
func ReadLayers(fsys fs.FS, name string) ([][]byte, error) {
	layers := []fs.FS{fsys}
	if l, ok := fsys.(*Layered); ok {
		layers = l.Layers()
	}
	var out [][]byte
	var firstErr error
	for _, layer := range layers {
		b, err := fs.ReadFile(layer, path.Clean(name))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, firstErr
	}
	return out, nil
}
