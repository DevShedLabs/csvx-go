# CSVX Go Engine

`csvx-go` is a Go library — the programmable interface for CSVX (load/edit/calculate/write). It is
specification-first: it must conform to CSVX behavior, but its internal architecture does not
define the format. **This repo is a library only** — its CLI was split into
[`csvx-cli`](https://github.com/DevShedLabs/csvx-cli), which depends on this repo as an ordinary Go
module. See `AGENTS.md` and `../csvx-spec/AGENTS.md` for why that split exists.

## Current scope

- CSVX ZIP package loading and writing (`Open`, `Load`, `WritePackage`)
- Unpacked CSVX directory loading/packaging (`OpenDirectory`, `PackageDirectory`, `ExtractPackage`)
- UTF-8 CSV sheet loading, RFC 4180-compatible
- Optional `.meta.json` sheet metadata: formulas, cached values, per-cell styles, validation
- `Style` generated from `../csvx-spec/schemas/styles.schema.json` (see `internal/schema/`), not
  hand-typed — see `AGENTS.md` for why that matters
- XLSX→CSVX import and unmodified-source recovery (`Convert`)
- Duplicate and unsafe ZIP entry rejection
- Basic structural validation (`Validate`)

Not yet implemented: formula parsing/recalculation, general CSVX→XLSX export of arbitrary/edited
content (only unmodified-source recovery exists today), full schema-conformance validation (that
lives in `../csvx-spec/validator` for now). See `handoff.md` for current known limitations.

## Development rule

Each capability follows this sequence:

```text
Specify → create conformance fixtures → implement → run tests
```

The specification repository is the authority: `../csvx-spec/`.

## Usage

```go
workbook, err := csvx.Open("report.csvx")
if err != nil {
    return err
}
fmt.Println(workbook.Sheets[0].Records)
```

CSV is the canonical sheet data layer. Metadata that CSV cannot represent is stored in the matching
`.meta.json` sidecar.

To exercise this library from the command line, use [`csvx-cli`](https://github.com/DevShedLabs/csvx-cli).

## Development

```bash
go build ./...                              # build
go vet ./...                                # static analysis
go test ./...                               # run all tests
go test -race ./...                         # with the race detector
go test -run TestLoadCSVBackedWorkbook ./... # a specific test
gofmt -w .                                   # format before committing
go mod tidy                                  # after changing imports — review the diff
```

### Pre-push checks (local, since GitHub Actions minutes are limited)

`scripts/check.sh` runs the same checks CI would (`gofmt`, `go vet`, `go build`, `go test`). Run it
any time:

```bash
./scripts/check.sh
```

A git hook runs it automatically before every push, blocking the push if it fails. Enable it once
per clone (this is local git config, not something that comes from cloning the repo):

```bash
git config core.hooksPath .githooks
```

Skip in a genuine emergency with `git push --no-verify` — prefer fixing the failure instead. See
`../csvx-spec/AGENTS.md` section 6 for why this exists: it's the interim stand-in for real CI.

## Development workflow

1. Update the relevant specification in `../csvx-spec/`.
2. Add or update a conformance fixture.
3. Implement the behavior in this library.
4. Run `./scripts/check.sh` (or let the pre-push hook do it).
5. Document any intentionally unsupported behavior in `handoff.md`.
6. Confirm that CSV data and metadata sidecars remain round-trip safe.

Do not treat engine behavior as a specification change without updating the specification repository.
