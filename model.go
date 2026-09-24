// Package csvx provides a specification-first reader and writer for CSVX workbooks.
package csvx

import "encoding/json"

// Manifest identifies a CSVX package and its resources.
type Manifest struct {
	Format   string   `json:"format"`
	Version  string   `json:"version"`
	Workbook string   `json:"workbook"`
	Files    []string `json:"files"`
}

// Workbook is the canonical in-memory representation of a CSVX workbook.
type Workbook struct {
	ID          string           `json:"id"`
	Version     string           `json:"version"`
	Sheets      []*Sheet         `json:"sheets"`
	Calculation Calculation      `json:"calculation,omitempty"`
	Source      *SourceMetadata              `json:"source,omitempty"`
	Styles      map[string]map[string]any     `json:"styles,omitempty"`
	SourceBytes []byte                        `json:"-"`
}

// SourceMetadata describes an embedded external workbook preserved for interoperability.
type SourceMetadata struct {
	Format     string            `json:"format"`
	Filename   string            `json:"filename"`
	SHA256     string            `json:"sha256"`
	Authority  string            `json:"authority"`
	ImportedAt string            `json:"importedAt"`
	Importer   string            `json:"importer"`
	Features   XLSXFeatureCounts `json:"features,omitempty"`
	Warnings   []XLSXDiagnostic  `json:"warnings,omitempty"`
}

// Calculation contains workbook calculation settings.
type Calculation struct {
	Mode      string `json:"mode,omitempty"`
	Iteration bool   `json:"iteration,omitempty"`
}

// Sheet is a CSV-backed worksheet. Records excludes the CSV header row.
type Sheet struct {
	ID           string                     `json:"id"`
	Name         string                     `json:"name"`
	Path         string                     `json:"path"`
	MetadataPath string                     `json:"metadata,omitempty"`
	Columns      []Column                   `json:"columns"`
	Records      [][]string                 `json:"records"`
	Cells        map[string]CellMetadata    `json:"cells,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

// Column describes a CSV column and its optional CSVX type.
type Column struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// CellMetadata contains behavior that cannot be represented in CSV.
type CellMetadata struct {
	Formula    string          `json:"formula,omitempty"`
	Cached     *Value          `json:"cached,omitempty"`
	Style      string          `json:"style,omitempty"`
	Validation json.RawMessage `json:"validation,omitempty"`
}

// Value is a typed CSVX value used by formulas, caches, and diagnostics.
type Value struct {
	Type    string `json:"type"`
	Value   any    `json:"value,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// SheetEntry locates a sheet's CSV and optional metadata sidecar in a package.
type SheetEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Metadata string `json:"metadata,omitempty"`
}

// WorkbookDocument is the serialized workbook resource.
type WorkbookDocument struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Sheets      []SheetEntry   `json:"sheets"`
	Calculation Calculation     `json:"calculation,omitempty"`
	Source      *SourceMetadata `json:"source,omitempty"`
	Styles      string          `json:"styles,omitempty"`
}
