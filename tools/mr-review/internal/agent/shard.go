package agent

import (
	"sort"
)

// ShardFiles splits files into at most k balanced, deterministic groups
// (sorted alphabetically, round-robin) for parallel review runs.
func ShardFiles(files []string, k int) [][]string {
	if k <= 1 || len(files) <= 1 {
		return [][]string{files}
	}
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	if k > len(sorted) {
		k = len(sorted)
	}
	groups := make([][]string, k)
	for i, f := range sorted {
		groups[i%k] = append(groups[i%k], f)
	}
	return groups
}
