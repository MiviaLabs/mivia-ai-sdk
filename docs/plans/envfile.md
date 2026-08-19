# envfile plan

## Goal

`envfile` loads a dotenv file into a map without ever leaking parsed
values into an error message.

## Scope

Inside:

- `Load(path string) (map[string]string, error)`: reads a file at
  `path`, parses `KEY=VALUE` lines, and returns the result as a map.
- Parsing rules: blank lines skip. A line starting with `#`, after
  leading whitespace, skips as a comment. A value wrapped in matching
  single or double quotes unwraps; a double-quoted value decodes
  standard Go escape sequences (`\n`, `\t`, `\\`, `\"`). An unquoted
  value keeps surrounding whitespace trimmed. A key must match
  `[A-Za-z_][A-Za-z0-9_]*`; any other key is a parse error.
- Errors: every error wraps a line number and, where relevant, a key
  name. No error ever includes a parsed value, quoted or unquoted.

Outside:

- Writing or exporting values into `os.Environ`. The caller decides
  what to do with the returned map.
- Variable expansion (`$FOO` substitution) or multi-line values.
- Any interpretation of the values (secrets, types). That is
  `secretpath`'s and the caller's job.

## API

- `Load(path string) (map[string]string, error)`

`Load` is the only exported symbol. A leaf package needs no
constructor or type: the map is the whole result, and one function
covers the one operation.

## Tests

- Table-driven parse cases: unquoted value, single-quoted value,
  double-quoted value with escapes, comment line, blank line,
  trailing comment stripped only outside quotes, empty value,
  whitespace around `=`.
- Malformed-key case, malformed-quote case (unterminated quote), and
  duplicate-key case (last write wins) each get their own test.
- A value containing a literal `=`, for example `KEY=a=b`, parses to
  `a=b`: only the first `=` on the line splits key from value.
- A file using CRLF line endings parses the same as its LF
  equivalent; each line's trailing `\r` strips before parsing.
- An error-message case asserts the returned error's `Error()` string
  never contains a literal value from any table case, including a
  deliberately sensitive-looking value like `s3cr3t-token`.
- Missing-file case returns a wrapped `os.ErrNotExist`, checked with
  `errors.Is`.

## Verification

- `make verify` passes: gofmt, vet, tests, doc gate, structure gate,
  Semgrep scan, and probes.
- Coverage for `envfile` reaches the 85 percent floor.
- `python3 scripts/check_plan.py` and `python3 scripts/check_deps.py`
  pass against this plan and the new `policy/layers.json` row.
