package ledger

import "errors"

// ErrLeaseActive is returned by Claim when another owner's lease has
// not yet reached its LeaseUntil deadline.
var ErrLeaseActive = errors.New("ledger: lease is still active")

// ErrFenced is returned by Renew, Release, or Complete when the
// caller's fence token no longer matches the stored record.
var ErrFenced = errors.New("ledger: fence token is stale")

// ErrNotStale is returned by Takeover when the current lease has not
// yet reached its LeaseUntil deadline.
var ErrNotStale = errors.New("ledger: lease is not stale")

// ErrNotClaimed is returned by Renew, Release, Complete, or Takeover
// when the stored record's Status is not StatusClaimed.
var ErrNotClaimed = errors.New("ledger: record is not claimed")

// ErrNoKey is returned by Claim, Renew, Release, Takeover, or
// Complete when the key has no admitted record.
var ErrNoKey = errors.New("ledger: key has no record")

// ErrUnknownStatus is returned by Complete when status is neither
// StatusCompleted nor StatusFailed.
var ErrUnknownStatus = errors.New("ledger: status must be StatusCompleted or StatusFailed")
