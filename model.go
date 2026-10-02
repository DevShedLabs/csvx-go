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

// MarshalJSON serializes a Style without the generated AdditionalProperties bucket field, which
// encoding/json would otherwise emit as a literal `"AdditionalProperties": null` key — schema-
// legal (styles.schema.json allows additional properties) but wrong output.
func (s Style) MarshalJSON() ([]byte, error) {
	type wire struct {
		ID           string                                `json:"id"`
		NumberFormat *string                               `json:"numberFormat,omitempty"`
		Font         schema.CSVXStylesStylesElemFont       `json:"font,omitempty"`
		Fill         schema.CSVXStylesStylesElemFill       `json:"fill,omitempty"`
		Border       *borderWire                           `json:"border,omitempty"`
		Alignment    schema.CSVXStylesStylesElemAlignment  `json:"alignment,omitempty"`
		Protection   schema.CSVXStylesStylesElemProtection `json:"protection,omitempty"`
	}
	return json.Marshal(wire{
		ID:           s.Id,
		NumberFormat: s.NumberFormat,
		Font:         s.Font,
		Fill:         s.Fill,
		Border:       borderToWire(s.Border),
		Alignment:    s.Alignment,
		Protection:   s.Protection,
	})
}

// borderWire / borderEdgeWire mirror the generated border types minus their AdditionalProperties
// bucket, for the same reason Style.MarshalJSON drops its own.
type borderWire struct {
	Style  *schema.BorderLineStyle `json:"style,omitempty"`
	Color  *string                 `json:"color,omitempty"`
	Top    *borderEdgeWire         `json:"top,omitempty"`
	Right  *borderEdgeWire         `json:"right,omitempty"`
	Bottom *borderEdgeWire         `json:"bottom,omitempty"`
	Left   *borderEdgeWire         `json:"left,omitempty"`
}

type borderEdgeWire struct {
	Style *schema.BorderLineStyle `json:"style,omitempty"`
	Color *string                 `json:"color,omitempty"`
}

func edgeToWire(edge *schema.BorderEdge) *borderEdgeWire {
	if edge == nil {
		return nil
	}
	return &borderEdgeWire{Style: edge.Style, Color: edge.Color}
}

// borderToWire returns nil for an entirely empty border so it is omitted from styles.json.
func borderToWire(border *schema.CSVXStylesStylesElemBorder) *borderWire {
	if border == nil {
		return nil
	}
	wire := borderWire{
		Style: border.Style, Color: border.Color,
		Top: edgeToWire(border.Top), Right: edgeToWire(border.Right),
		Bottom: edgeToWire(border.Bottom), Left: edgeToWire(border.Left),
	}
	if wire == (borderWire{}) {
		return nil
	}
	return &wire
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
	Calculation Calculation     `json:"calculation,omitempty"`
	Source      *SourceMetadata `json:"source,omitempty"`
	Styles      []Style         `json:"styles,omitempty"`
	SourceBytes []byte          `json:"-"`
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
	Calculation Calculation     `json:"calculation,omitempty"`
	Source      *SourceMetadata `json:"source,omitempty"`
	Styles      string          `json:"styles,omitempty"`
}
