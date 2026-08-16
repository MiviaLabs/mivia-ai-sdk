# Package reference: room

The room package manages standing groups for messages: the roster
that `envelope.Message.Room` only names. It holds membership, roles,
and message admission. The exported surface below mirrors
`api/room.txt`.

## Types

- `Room` — a named standing group with a role roster. It is safe for
  concurrent use.
- `Role` — a member's power in a room. Constants: `RoleMember`,
  `RoleModerator`.

## Functions and methods

- `New(id, founder)` — creates a room with the founder as its first
  moderator.
- `Room.ID()` — the room name that messages must carry.
- `Room.IsMember(id)` — reports whether the identity is on the
  roster.
- `Room.Members()` — the sorted roster.
- `Room.Admit(id, by)` — adds a member; `by` must be a moderator.
- `Room.Remove(id, by)` — drops a member; `by` must be a moderator.
- `Room.Leave(id)` — drops the identity by its own choice.
- `Room.Promote(id, by)` — raises a member to moderator.
- `Room.Accepts(m)` — gates a message on signer and recipients.

## Sentinel errors

Use `errors.Is` to test these.

- `ErrNotMember` — the identity is not on the roster.
- `ErrNotModerator` — the actor lacks moderator power.
- `ErrAlreadyMember` — the identity is already on the roster.
- `ErrLastModerator` — the last moderator cannot be removed or leave.
- `ErrWrongRoom` — the message names a different room.
- `ErrUnsigned` — the message is unsigned or the signature fails.

## Invariants

`New` and the roster methods enforce the rules below.

- `New` requires a non-blank id and founder.
- The founder becomes the first moderator.
- `Admit`, `Remove`, and `Promote` require a moderator actor.
- `Admit` rejects a blank member id and a duplicate member.
- `Remove`, `Leave`, and `Promote` reject a non-member.
- The last moderator cannot be removed or leave.
- `Accepts` requires the message room to match.
- `Accepts` requires a valid, signed message from a member.
- `Accepts` requires every recipient to be a member.
- `Members` returns the sorted roster.

## Wire contract

- `Room` has no JSON wire form; the roster lives in memory.
- The room name travels in `envelope.Message.Room`.
- Admission never reads the wire bytes; it uses the validated message.

## Usage

```go
_, founderKey, _ := ed25519.GenerateKey(nil)
_, bobKey, _ := ed25519.GenerateKey(nil)
founderHex := hex.EncodeToString(founderKey.Public().(ed25519.PublicKey))
bobHex := hex.EncodeToString(bobKey.Public().(ed25519.PublicKey))

r, _ := room.New("platform-team", founderHex)
_ = r.Admit(bobHex, founderHex)
msg := envelope.Message{
    Version:   envelope.Version,
    ID:        "msg-1",
    Room:      "platform-team",
    ThreadID:  "task-42",
    Intent:    envelope.IntentRequest,
    Epistemic: envelope.EpistemicInferred,
    To:        []string{bobHex},
    Payload:   "Please review the plan.",
}
msg, _ = envelope.Sign(bobKey, msg)
_ = r.Accepts(msg)
```
