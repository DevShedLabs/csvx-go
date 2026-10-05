// Package csvx provides a specification-first reader and writer for CSVX workbooks.
package csvx

import (
	"encoding/json"

	"github.com/DevShedLabs/csvx-go/internal/schema"
)

// Style is a single style record. Its field shape is generated from csvx-spec's
// styles.schema.json (see internal/schema/generated.go) rather than hand-typed, so it cannot
// silently drift from the schema the way the previous hand-written map[string]map[string]any
// representation did.
//
// This is a defined type over the generated struct, not a plain alias, solely so MarshalJSON
// below can omit the generated AdditionalProperties bucket field until this engine implements
// real additionalProperties round-tripping on both read and write (styles.json is not read back
// in anywhere yet, so nothing currently depends on inheriting the generated UnmarshalJSON here).
type Style schema.CSVXStylesStylesElem

// UnmarshalJSON decodes through the generated type so the id pattern is enforced and properties the
// schema does not define land in AdditionalProperties (rule 3.6: unknown fields round-trip).
func (s *Style) UnmarshalJSON(data []byte) error {
	var decoded schema.CSVXStylesStylesElem
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	// The generated decoder keeps unknown properties of the style itself, but not those nested in
	// border or its edges (`diagonal`, an importer's `xlsxStyle`); capture them here so they round-trip
	// (spec/08-styles.md).
	var raw struct {
		Border map[string]json.RawMessage `json:"border"`
	}
	if err := json.Unmarshal(data, &raw); err == nil && decoded.Border != nil {
		known := map[string]bool{"style": true, "color": true, "top": true, "right": true, "bottom": true, "left": true}
		if extra := unknownKeys(raw.Border, known); len(extra) > 0 {
			decoded.Border.AdditionalProperties = extra
		}
		edges := map[string]*schema.BorderEdge{"top": decoded.Border.Top, "right": decoded.Border.Right, "bottom": decoded.Border.Bottom, "left": decoded.Border.Left}
		for name, edge := range edges {
			if edge == nil {
				continue
			}
			var rawEdge map[string]json.RawMessage
			if json.Unmarshal(raw.Border[name], &rawEdge) == nil {
				if extra := unknownKeys(rawEdge, map[string]bool{"style": true, "color": true}); len(extra) > 0 {
					edge.AdditionalProperties = extra
				}
			}
		}
	}
	*s = Style(decoded)
	return nil
}

// unknownKeys decodes the entries of raw whose names are not in known.
func unknownKeys(raw map[string]json.RawMessage, known map[string]bool) map[string]any {
	extra := map[string]any{}
	for key, value := range raw {
		if known[key] {
			continue
		}
		var decoded any
		if json.Unmarshal(value, &decoded) == nil {
			extra[key] = decoded
		}
	}
	return extra
}

// MarshalJSON writes the typed fields plus any preserved unknown properties (at the style level and
// inside border), without the generated AdditionalProperties bucket key itself.
func (s Style) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(schema.CSVXStylesStylesElem(s))
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(flattenAdditionalProperties(generic))
}

// NamedRange is a workbook-scoped name (spec/02-workbook.md, Named ranges). Like Style, it is a
// defined type over the generated struct so unknown properties round-trip (rule 3.6).
type NamedRange schema.CSVXWorkbookNamedRangesElem

// UnmarshalJSON decodes through the generated type so the name pattern is enforced and unknown
// properties land in AdditionalProperties.
func (n *NamedRange) UnmarshalJSON(data []byte) error {
	var decoded schema.CSVXWorkbookNamedRangesElem
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*n = NamedRange(decoded)
	return nil
}

// MarshalJSON writes the typed fields plus preserved unknown properties, without the generated
// AdditionalProperties bucket key itself.
func (n NamedRange) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(schema.CSVXWorkbookNamedRangesElem(n))
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(flattenAdditionalProperties(generic))
}

// Manifest identifies a CSVX package and its resources.
type Manifest struct {
	Format   string   `json:"format"`
	Version  string   `json:"version"`
	Workbook string   `json:"workbook"`
	Files    []string `json:"files"`
}

// Workbook is the canonical in-memory representation of a CSVX workbook.
type Workbook struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Sheets      []*Sheet        `json:"sheets"`
	NamedRanges []NamedRange    `json:"namedRanges,omitempty"`
	Calculation Calculation     `json:"calculation,omitempty"`
	Source      *SourceMetadata `json:"source,omitempty"`
	Styles      []Style         `json:"styles,omitempty"`
	SourceBytes []byte          `json:"-"`

	// importWarnings are importer findings not yet attached to Source (see importXLSXSource).
	importWarnings []XLSXDiagnostic
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
	RowHeights   map[int]float64            `json:"rowHeights,omitempty"`
	Print        *PrintSettings             `json:"print,omitempty"`
	Cells        map[string]CellMetadata    `json:"cells,omitempty"`
	Extra        map[string]json.RawMessage `json:"-"`
}

// Column describes a CSV column and its optional CSVX type.
type Column struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Type  string  `json:"type,omitempty"`
	Width float64 `json:"width,omitempty"`
}

// CellMetadata contains behavior that cannot be represented in CSV.
type CellMetadata struct {
	Type       string          `json:"type,omitempty"`
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
	Sheets      []SheetEntry    `json:"sheets"`
	NamedRanges []NamedRange    `json:"namedRanges,omitempty"`
	Calculation Calculation     `json:"calculation,omitempty"`
	Source      *SourceMetadata `json:"source,omitempty"`
	Styles      string          `json:"styles,omitempty"`
}
