# CSVX Go Engine

The Go engine is the first implementation of the CSVX specification. It is intentionally
specification-first: the engine must conform to CSVX behavior, but its internal architecture does
not define the format.

## Current scope

The initial package provides the Phase 1 foundation:

- CSVX ZIP package loading
- Manifest and workbook loading
- UTF-8 CSV sheet loading
- Optional `.meta.json` sheet metadata
- Typed metadata structures for columns, formulas, caches, styles, and validation
- Sparse cell metadata addressed by A1 coordinates
- Duplicate and unsafe ZIP entry rejection
- Package and extract CLI commands for developer workflows

Formula parsing, calculation, import/export, and full CLI operations will be added behind the
same canonical workbook model.

## Development rule

Each capability follows this sequence:

```text
Specify → create conformance fixtures → implement → run tests
```

The specification repository is the authority:

```text
../csvx-spec/
```

## Package

```go
workbook, err := csvx.Open("report.csvx")
if err != nil {
    return err
}
fmt.Println(workbook.Sheets[0].Records)
```

CSV is the canonical sheet data layer. Metadata that CSV cannot represent is stored in the matching
`.meta.json` sidecar.

## Commands

Run commands from the repository root:

### Format

```bash
gofmt -w .
```

Formats all Go source files before committing.

### Build

```bash
go build ./...
```

Builds every package in the module.

To build the CLI once it is added:

```bash
go build -o bin/csvx ./cmd/csvx
```

### Test

```bash
go test ./...
```

Runs all unit and package tests.

Run tests with the race detector:

```bash
go test -race ./...
```

Run a specific test:

```bash
go test -run TestLoadCSVBackedWorkbook ./...
```

### Coverage

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Static analysis

```bash
go vet ./...
```

### Run the CLI

Build the current CLI:

```bash
go build -o bin/csvx ./cmd/csvx
```

The currently supported commands are:

```bash
csvx --help
csvx version
csvx inspect report.csvx
csvx inspect ../csvx-spec/examples/minimal.csvx
csvx validate report.csvx
csvx validate ../csvx-spec/examples/minimal.csvx
csvx package ../csvx-spec/examples/minimal.csvx --output minimal.csvx
csvx extract minimal.csvx --output minimal-extracted
```

Both `.csvx` ZIP files and unpacked CSVX package directories are accepted. The following commands
are planned but not implemented yet:

```bash
csvx recalc report.csvx
csvx convert report.xlsx report.csvx
csvx convert report.csvx report.xlsx
csvx convert report.csvx report.csv
```

### Dependency and module maintenance

```bash
go mod tidy
go list -m all
go version
```

`go mod tidy` should be run when imports change. Review its changes before committing.

## Development workflow

1. Update the relevant specification in `../csvx-spec/`.
2. Add or update a conformance fixture.
3. Implement the behavior in the engine.
4. Run `gofmt`, `go vet`, and `go test ./...`.
5. Document any intentionally unsupported behavior.
6. Confirm that CSV data and metadata sidecars remain round-trip safe.

Do not treat engine behavior as a specification change without updating the specification repository.

