package git

import "strings"

// ListFiles returns the repo's tracked files (repo-relative paths) via
// `git ls-files`. A non-zero exit with empty output is treated as no files,
// not an error (mirrors Grep's tolerance).
func ListFiles(r Runner, dir string) ([]string, error) {
	out, err := r.Run(dir, "ls-files")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
