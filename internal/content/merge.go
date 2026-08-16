package content

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// MergeJSON reads a JSON array from every layer of fsys and merges the
// entries by key: an upper-layer entry replaces the lower one with the same
// key in place, new keys are appended in their file order. A content file
// therefore only needs the entries it adds or changes.
func MergeJSON[T any](fsys fs.FS, name string, key func(T) string) ([]T, error) {
	layers, err := ReadLayers(fsys, name)
	if err != nil {
		return nil, err
	}
	var out []T
	index := map[string]int{}
	for _, data := range layers {
		var list []T
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, item := range list {
			k := key(item)
			if i, ok := index[k]; ok && k != "" {
				out[i] = item
				continue
			}
			index[k] = len(out)
			out = append(out, item)
		}
	}
	return out, nil
}
