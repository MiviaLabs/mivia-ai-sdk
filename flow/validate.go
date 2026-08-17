package flow

// validateSteps checks every step ID and every dependency edge.
// It fills ids with each step ID mapped to its index. It rejects an
// empty or duplicate ID, and a dependency that names a missing step.
func validateSteps(steps []Step, ids map[string]int) error {
	for i := range steps {
		id := steps[i].ID
		if id == "" {
			return errorf("step %d has an empty ID", i)
		}
		if _, dup := ids[id]; dup {
			return errorf("duplicate step ID %q", id)
		}
		ids[id] = i
	}
	for i := range steps {
		for _, need := range steps[i].Needs {
			if _, ok := ids[need]; !ok {
				return errorf("step %q needs unknown step %q", steps[i].ID, need)
			}
		}
	}
	return nil
}

// validatePanels checks every panel entry. Every entry must name a
// step ID that exists in the graph.
func validatePanels(panels []Panel, ids map[string]int) error {
	for i, p := range panels {
		for _, id := range p {
			if _, ok := ids[id]; !ok {
				return errorf("panel %d names unknown step %q", i, id)
			}
		}
	}
	return nil
}

// findRoots computes the root step IDs with Kahn's algorithm.
// It returns the roots in declaration order, or an error when a
// cycle leaves nodes unprocessed. A step with no Needs is a root.
func findRoots(steps []Step, ids map[string]int) ([]string, error) {
	n := len(steps)
	indeg := make([]int, n)
	next := make([][]int, n)
	var roots []string
	for i := range steps {
		seen := map[int]bool{}
		for _, need := range steps[i].Needs {
			if seen[ids[need]] {
				continue
			}
			seen[ids[need]] = true
			next[ids[need]] = append(next[ids[need]], i)
			indeg[i]++
		}
		if indeg[i] == 0 {
			roots = append(roots, steps[i].ID)
		}
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	for pos := 0; pos < len(queue); pos++ {
		v := queue[pos]
		for _, to := range next[v] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	if len(queue) != n {
		return nil, errorf("cycle detected in step graph")
	}
	return roots, nil
}
