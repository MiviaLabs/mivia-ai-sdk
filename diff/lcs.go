package diff

// op is one line in a computed diff: an unchanged line (kind ' '), a
// removed line (kind '-'), or an added line (kind '+'). aIdx and bIdx
// are the 0-based index of the line in a and b, or -1 when the line
// has no counterpart on that side.
type op struct {
	kind byte
	text string
	aIdx int
	bIdx int
}

// computeOps aligns a and b with a longest-common-subsequence match,
// computed with a dynamic-programming table, and returns the ordered
// edit script that turns a into b.
func computeOps(a, b []string) []op {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}

	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{kind: ' ', text: a[i], aIdx: i, bIdx: j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{kind: '-', text: a[i], aIdx: i, bIdx: -1})
			i++
		default:
			ops = append(ops, op{kind: '+', text: b[j], aIdx: -1, bIdx: j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{kind: '-', text: a[i], aIdx: i, bIdx: -1})
	}
	for ; j < m; j++ {
		ops = append(ops, op{kind: '+', text: b[j], aIdx: -1, bIdx: j})
	}
	return ops
}
