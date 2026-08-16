# Plan: room

## Goal

Standing groups: the roster that envelope.Message.Room only names.
Membership, roles, and admission of messages.

## Scope

Inside: Room roster with moderator/member roles, moderator-gated Admit/
Remove/Promote, Leave, last-moderator protection, Accepts admission
gate (signature verification plus membership of signer and recipients).
Outside: message semantics (envelope), persistence, federation.

## API

Room, Role types; New, sentinel errors; Admit/Remove/Promote/Leave/
IsMember/Members/ID/Accepts methods. Locked in `api/room.txt`.
Imports only envelope (policy/layers.json). Accepts verifies
signatures itself so callers cannot skip authentication. The lock
gains the six sentinel `var` lines when api_surface learns vars;
see gates.md (C1). No symbol change.

## Tests

Unit tables for every guard (non-moderator, stranger, duplicate, last
moderator), role-transition flows, and end-to-end group integration:
signed posting, attributed acks, thread chains, forgery rejection,
post-removal rejection, admission failure table.

Add a concurrent roster stress test: goroutines mix Admit, Promote,
Leave, Remove, and Accepts against one Room. Synchronize with
sync.WaitGroup; never time.Sleep. This makes `go test -race` exercise
the mutex-guarded roster instead of passing vacuously.

## Verification

`make verify` plus `go test -race ./...` for the mutex-guarded roster.
