package ledger

// evictScanBudget bounds the protected heads one eviction round
// rotates before it returns. The value is arbitrary: it is a scan
// budget chosen to clear a short run of live leases in one round, not
// a derived threshold.
const evictScanBudget = 8

// evictOverCap deletes queued records while the entry count exceeds
// maxEntries. It is a no-op when maxEntries is zero (unbounded). A
// protected head rotates to the tail: exempt is the key the current
// write touches, and a StatusClaimed record whose LeaseUntil is after
// the store's clock holds a live lease. Deletion raises fenceFloor to
// the deleted record's Fence. The caller must hold m.mu.
func (m *MemStore) evictOverCap(exempt IdempotencyKey) {
	if m.maxEntries <= 0 {
		return
	}
	rotations := 0
	for len(m.tasks) > m.maxEntries {
		k := m.evictQueue[0]
		m.evictQueue = m.evictQueue[1:]
		rec := m.tasks[k]
		if k == exempt || (rec.Status == StatusClaimed && rec.LeaseUntil.After(m.now())) {
			m.evictQueue = append(m.evictQueue, k)
			rotations++
			if rotations >= evictScanBudget {
				return
			}
			continue
		}
		if rec.Fence > m.fenceFloor {
			m.fenceFloor = rec.Fence
		}
		delete(m.tasks, k)
	}
}
