// Package symbols extracts a read-only summary of a repository's public
// surface: Go "main" package directories and exported top-level names, plus
// npm scripts and bin entries declared in package.json.
package symbols

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// SymbolSet is a read-only summary of a repository's symbols.
type SymbolSet struct {
	GoMainPkgs []string `json:"goMainPkgs"`
	GoExported []string `json:"goExported"`
	NpmScripts []string `json:"npmScripts"`
	NpmBin     []string `json:"npmBin"`
	Truncated  bool     `json:"truncated"`
}

// skipDirs are not descended into while walking for Go/npm sources.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
}

// maxGoFiles caps the number of .go files parsed before Extract gives up
// and reports Truncated.
const maxGoFiles = 400

// Extract walks dir and returns a SymbolSet describing its Go main packages,
// exported top-level Go names, and npm scripts/bin entries. It never
// returns an error: unreadable or unparsable inputs are simply skipped.
func Extract(dir string) SymbolSet {
	mainPkgs := map[string]bool{}
	exported := map[string]bool{}

	fset := token.NewFileSet()
	parsedCount := 0
	truncated := false

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".go" {
			return nil
		}
		if len(d.Name()) >= 8 && d.Name()[len(d.Name())-8:] == "_test.go" {
			return nil
		}

		if parsedCount >= maxGoFiles {
			truncated = true
			return filepath.SkipAll
		}

		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		parsedCount++

		if file.Name.Name == "main" {
			rel, rerr := filepath.Rel(dir, filepath.Dir(path))
			if rerr == nil {
				rel = filepath.ToSlash(rel)
				if rel == "" {
					rel = "."
				}
				mainPkgs[rel] = true
			}
		}

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && ast.IsExported(d.Name.Name) {
					exported[d.Name.Name] = true
				}
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if ok && ast.IsExported(ts.Name.Name) {
							exported[ts.Name.Name] = true
						}
					}
				}
			}
		}

		return nil
	})

	npmScripts, npmBin := readPackageJSON(dir)

	return SymbolSet{
		GoMainPkgs: sortedKeys(mainPkgs),
		GoExported: sortedKeys(exported),
		NpmScripts: npmScripts,
		NpmBin:     npmBin,
		Truncated:  truncated,
	}
}

// packageJSON is the subset of package.json fields Extract cares about.
type packageJSON struct {
	Name    string          `json:"name"`
	Scripts map[string]any  `json:"scripts"`
	Bin     json.RawMessage `json:"bin"`
}

func readPackageJSON(dir string) (scripts []string, bin []string) {
	scriptSet := map[string]bool{}
	binSet := map[string]bool{}

	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err == nil {
		var pkg packageJSON
		if json.Unmarshal(data, &pkg) == nil {
			for k := range pkg.Scripts {
				scriptSet[k] = true
			}
			if len(pkg.Bin) > 0 {
				var asString string
				var asObject map[string]any
				if json.Unmarshal(pkg.Bin, &asString) == nil {
					if pkg.Name != "" {
						binSet[pkg.Name] = true
					}
				} else if json.Unmarshal(pkg.Bin, &asObject) == nil {
					for k := range asObject {
						binSet[k] = true
					}
				}
			}
		}
	}

	return sortedKeys(scriptSet), sortedKeys(binSet)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
