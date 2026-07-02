package git

import (
	"fmt"
	"sort"
	"strings"
)

// DayCount is the number of commits on a single day.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// CommitActivity returns per-day commit counts for the last `weeks` weeks,
// sorted ascending by date, from the repo's git log.
func CommitActivity(r Runner, dir string, weeks int) ([]DayCount, error) {
	since := fmt.Sprintf("%d days ago", weeks*7)
	out, err := r.Run(dir, "log", "--since="+since, "--date=short", "--pretty=format:%cd")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		d := strings.TrimSpace(line)
		if d != "" {
			counts[d]++
		}
	}
	dates := make([]string, 0, len(counts))
	for d := range counts {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	res := make([]DayCount, 0, len(dates))
	for _, d := range dates {
		res = append(res, DayCount{Date: d, Count: counts[d]})
	}
	return res, nil
}
