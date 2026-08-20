package ledger

import "errors"

// ErrLeaseActive is returned by Claim when the stored LeaseUntil is
// still after now, whichever owner holds the lease.
var ErrLeaseActive = errors.New("ledger: lease is still active")

// ErrFenced is returned by Renew, Release, or Complete when the
// caller's fence token no longer matches the stored record.
var ErrFenced = errors.New("ledger: fence token is stale")

// ErrNotStale is returned by Takeover when the current lease has not
// yet reached its LeaseUntil deadline.
var ErrNotStale = errors.New("ledger: lease is not stale")

// ErrNotClaimed is returned by Claim, Renew, Release, Complete, or
// Takeover for a record the caller may not hold. Renew, Release, and
// Complete return it when the stored record's Status is not
// StatusClaimed. Claim returns it for a terminal or blocked record,
// and Takeover for a StatusPending or terminal record. Claim and
// Takeover also return it for an otherwise eligible record, including
// a StatusPending one, when a key in its transitive Needs closure
// holds StatusFailed or StatusBlocked.
var ErrNotClaimed = errors.New("ledger: record is not claimed")

// ErrNoKey is returned by Claim, Renew, Release, Takeover, or
// Complete when the key has no admitted record.
var ErrNoKey = errors.New("ledger: key has no record")

// ErrUnknownStatus is returned by Complete when status is neither
// StatusCompleted nor StatusFailed.
var ErrUnknownStatus = errors.New("ledger: status must be StatusCompleted or StatusFailed")

// ErrEmptyOwner is returned by Claim or Takeover when owner is empty.
var ErrEmptyOwner = errors.New("ledger: owner must not be empty")

// ErrInvalidMaxEntries is returned by NewMemStoreWithOptions when
// MemStoreOptions.MaxEntries is negative.
var ErrInvalidMaxEntries = errors.New("ledger: MaxEntries must not be negative")
