package ledger

import "github.com/MiviaLabs/mivia-ai-sdk/events"

// AdmittedEvent fires once per successful Admit.
const AdmittedEvent events.Name = "ledger.admitted"

// ClaimedEvent fires once per successful Claim.
const ClaimedEvent events.Name = "ledger.claimed"

// RenewedEvent fires once per successful Renew.
const RenewedEvent events.Name = "ledger.renewed"

// ReleasedEvent fires once per successful Release.
const ReleasedEvent events.Name = "ledger.released"

// TakenOverEvent fires once per successful Takeover.
const TakenOverEvent events.Name = "ledger.taken_over"

// CompletedEvent fires once per successful Complete.
const CompletedEvent events.Name = "ledger.completed"

// BlockedEvent fires once per dependent a failed Complete blocks.
const BlockedEvent events.Name = "ledger.blocked"
