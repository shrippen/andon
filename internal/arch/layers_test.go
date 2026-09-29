package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbidden: what a layer's (non-test) code must not import, so it only
// talks to its neighbour below.
var forbidden = map[string][]string{
	"web":      {"andon/internal/repos/", "andon/internal/sources", "andon/internal/drivers/", "andon/internal/outbound", "andon/internal/db"},
	"services": {"andon/internal/drivers/"},
	"widgets":  {"andon/internal/services/", "andon/internal/repos/", "andon/internal/drivers/", "andon/internal/db"},
	"metrics":  {"andon/internal/services/", "andon/internal/repos/", "andon/internal/drivers/", "andon/internal/db"},
	"rules":    {"andon/internal/services/", "andon/internal/repos/", "andon/internal/drivers/", "andon/internal/db"},
}

func TestLayers(t *testing.T) {
	for layer, banned := range forbidden {
		root := filepath.Join("..", layer)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				for _, b := range banned {
					if name == strings.TrimSuffix(b, "/") || strings.HasPrefix(name, b) {
						t.Errorf("%s imports %s", path, name)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
