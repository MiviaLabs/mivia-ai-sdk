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

// validatePanels checks every panel entry. Within one panel the check
// order is fixed: every member's ID must resolve to a known step
// first; no member ID may repeat second; every member's To must equal
// the first member's To last. After that loop, no step ID may be
// named in two panels. The homogeneity check keeps a wave's
// resulting status well-defined: every member fires from one shared
// cur and lands on one shared To, never several.
func validatePanels(panels []Panel, steps []Step, ids map[string]int) error {
	for i, p := range panels {
		for _, id := range p {
			if _, ok := ids[id]; !ok {
				return errorf("panel %d names unknown step %q", i, id)
			}
		}
		seen := map[string]bool{}
		for _, id := range p {
			if seen[id] {
				return errorf("panel %d names step %q twice", i, id)
			}
			seen[id] = true
		}
		if err := validatePanelHomogeneity(i, p, steps, ids); err != nil {
			return err
		}
	}
	return validatePanelOverlap(panels)
}

// validatePanelOverlap rejects a step ID named by two panels. The
// scan walks panels in declaration order and members in member
// order. It maps each ID to the first panel index that named it. The
// first member found again fails.
func validatePanelOverlap(panels []Panel) error {
	first := map[string]int{}
	for i, p := range panels {
		for _, id := range p {
			j, ok := first[id]
			if !ok {
				first[id] = i
				continue
			}
			return errorf("step %q is named in panels %d and %d", id, j, i)
		}
	}
	return nil
}

// validatePanelHomogeneity checks that every member of p shares the
// first member's To. It runs only after every member of p resolves
// to a known step; it reads a member's To through steps[ids[id]].
func validatePanelHomogeneity(i int, p Panel, steps []Step, ids map[string]int) error {
	if len(p) == 0 {
		return nil
	}
	first := p[0]
	firstTo := steps[ids[first]].To
	for _, id := range p[1:] {
		to := steps[ids[id]].To
		if to != firstTo {
			return errorf(
				"panel %d: step %q and step %q disagree on To (%q vs %q)",
				i, first, id, firstTo, to,
			)
		}
	}
	return nil
}

// validatePanelIndependence rejects a panel whose member's transitive
// Needs closure reaches a fellow member of the same panel. It runs
// after findRoots proves the step graph acyclic; walking a closure
// needs an acyclic graph, so this check cannot loop forever.
func validatePanelIndependence(steps []Step, panels []Panel, ids map[string]int) error {
	needs := make(map[string][]string, len(steps))
	for _, s := range steps {
		needs[s.ID] = s.Needs
	}
	memo := map[string]map[string]bool{}
	for i, p := range panels {
		for _, id := range p {
			anc := ancestorsOf(id, needs, memo)
			for _, other := range p {
				if other == id {
					continue
				}
				if anc[other] {
					return errorf(
						"panel %d: step %q needs step %q, a member of the same panel",
						i, id, other,
					)
				}
			}
		}
	}
	return nil
}

// validatePayload rejects a step that sets both Payload and
// PayloadFrom, and a PayloadFrom on a member of a panel of two or more
// members. A one-member panel keeps the field, because Run still calls
// Confirm for its single member, so the field never stays a silent
// no-op.
func validatePayload(steps []Step, panels []Panel, ids map[string]int) error {
	for i := range steps {
		if steps[i].PayloadFrom != nil && steps[i].Payload != "" {
			return errorf("step %q sets both Payload and PayloadFrom", steps[i].ID)
		}
	}
	for i, p := range panels {
		if len(p) < 2 {
			continue
		}
		for _, id := range p {
			if steps[ids[id]].PayloadFrom != nil {
				return errorf("panel %d names payload-from step %q", i, id)
			}
		}
	}
	return nil
}

// ancestorsOf returns the transitive Needs closure of id: every step
// id depends on, directly or through another dependency. It memoizes
// each ID's closure so a shared dependency is walked once.
func ancestorsOf(id string, needs map[string][]string, memo map[string]map[string]bool) map[string]bool {
	if a, ok := memo[id]; ok {
		return a
	}
	a := map[string]bool{}
	for _, need := range needs[id] {
		a[need] = true
		for anc := range ancestorsOf(need, needs, memo) {
			a[anc] = true
		}
	}
	memo[id] = a
	return a
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
