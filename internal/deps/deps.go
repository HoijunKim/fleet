// Package deps derives dependency relationships between local repos from their
// go.mod / package.json manifests.
package deps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type RepoRef struct{ ID, Path, Name string }
type Node struct {
	ID       string
	Name     string
	Produces []string
}
type Edge struct{ From, To string }
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Produces returns the Go module path (go.mod) and npm package name
// (package.json) this directory publishes; either may be "".
func Produces(dir string) (goModule, jsName string) {
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "module ") {
				goModule = strings.TrimSpace(strings.TrimPrefix(line, "module "))
				break
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			jsName = pkg.Name
		}
	}
	return
}

// Requires returns dependency names declared here: go.mod require module paths
// + package.json dependencies/devDependencies keys.
func Requires(dir string) []string {
	var out []string
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		out = append(out, parseGoRequires(string(data))...)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			for k := range pkg.Dependencies {
				out = append(out, k)
			}
			for k := range pkg.DevDependencies {
				out = append(out, k)
			}
		}
	}
	return out
}

func parseGoRequires(gomod string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "":
			out = append(out, firstField(line))
		case strings.HasPrefix(line, "require "):
			out = append(out, firstField(strings.TrimPrefix(line, "require ")))
		}
	}
	return out
}

func firstField(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// BuildGraph maps each repo's produced module/name, then emits an edge from a
// repo to another repo whose produced name it requires. Nodes are returned for
// every input repo; edges are de-duplicated.
func BuildGraph(repos []RepoRef) Graph {
	owner := map[string]string{} // produced name -> repo id
	nodes := make([]Node, 0, len(repos))
	for _, r := range repos {
		gm, js := Produces(r.Path)
		var prod []string
		if gm != "" {
			owner[gm] = r.ID
			prod = append(prod, gm)
		}
		if js != "" {
			owner[js] = r.ID
			prod = append(prod, js)
		}
		nodes = append(nodes, Node{ID: r.ID, Name: r.Name, Produces: prod})
	}
	seen := map[string]bool{}
	edges := []Edge{}
	for _, r := range repos {
		for _, dep := range Requires(r.Path) {
			if to, ok := owner[dep]; ok && to != r.ID {
				key := r.ID + "\x00" + to
				if !seen[key] {
					seen[key] = true
					edges = append(edges, Edge{From: r.ID, To: to})
				}
			}
		}
	}
	return Graph{Nodes: nodes, Edges: edges}
}
