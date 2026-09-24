package csvx

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// XLSXInspection describes the portable features and package resources detected in an XLSX file.
type XLSXInspection struct {
	Format   string            `json:"format"`
	Filename string            `json:"filename"`
	SHA256   string            `json:"sha256"`
	Sheets   []XLSXSheet       `json:"sheets"`
	Features XLSXFeatureCounts `json:"features"`
	Resources []string         `json:"resources"`
	Warnings []XLSXDiagnostic  `json:"warnings"`
}

// XLSXSheet describes one worksheet and its understood cell features.
type XLSXSheet struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Cells         int    `json:"cells"`
	Formulas      int    `json:"formulas"`
	CachedValues  int    `json:"cachedValues"`
	StyledCells   int    `json:"styledCells"`
	SharedStrings int    `json:"sharedStrings"`
}

// XLSXFeatureCounts summarizes workbook features relevant to CSVX interoperability.
type XLSXFeatureCounts struct {
	SharedStrings int `json:"sharedStrings"`
	Styles        int `json:"styles"`
	NumberFormats int `json:"numberFormats"`
	Drawings      int `json:"drawings"`
	Charts        int `json:"charts"`
	Images        int `json:"images"`
	Comments      int `json:"comments"`
	Persons       int `json:"persons"`
	Macros        int `json:"macros"`
	Relationships int `json:"relationships"`
}

// XLSXDiagnostic records an interoperability limitation or observation.
type XLSXDiagnostic struct {
	Severity string `json:"severity"`
	Feature  string `json:"feature"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

// InspectXLSX reads an XLSX package without executing macros, links, or external resources.
func InspectXLSX(filename string) (*XLSXInspection, error) {
	body, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read XLSX: %w", err)
	}
	archive, err := zip.NewReader(strings.NewReader(string(body)), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("read XLSX ZIP: %w", err)
	}
	files := map[string][]byte{}
	for _, entry := range archive.File {
		if entry.Name == "" || path.IsAbs(entry.Name) || strings.HasPrefix(path.Clean(entry.Name), "..") {
			return nil, fmt.Errorf("unsafe XLSX entry %q", entry.Name)
		}
		reader, readErr := entry.Open()
		if readErr != nil {
			return nil, fmt.Errorf("open XLSX entry %q: %w", entry.Name, readErr)
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read XLSX entry %q: %w", entry.Name, readErr)
		}
		files[entry.Name] = content
	}
	inspection := &XLSXInspection{Format: "xlsx", Filename: path.Base(filename), SHA256: hashBytes(body), Resources: sortedResources(files)}
	inspection.Features = countFeatures(files)
	inspection.Sheets = inspectSheets(files, inspection.Features.SharedStrings)
	addFeatureWarnings(inspection)
	return inspection, nil
}

func hashBytes(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func sortedResources(files map[string][]byte) []string {
	resources := make([]string, 0, len(files))
	for name := range files { resources = append(resources, name) }
	for i := 1; i < len(resources); i++ { for j := i; j > 0 && resources[j] < resources[j-1]; j-- { resources[j], resources[j-1] = resources[j-1], resources[j] } }
	return resources
}

func countFeatures(files map[string][]byte) XLSXFeatureCounts {
	var counts XLSXFeatureCounts
	for name, body := range files {
		text := string(body)
		switch {
		case name == "xl/sharedStrings.xml": counts.SharedStrings = strings.Count(text, "<si")
		case name == "xl/styles.xml": counts.Styles = strings.Count(text, "<xf") - 1; counts.NumberFormats = strings.Count(text, "<numFmt")
		case strings.Contains(name, "drawing"): counts.Drawings++
		case strings.Contains(name, "chart"): counts.Charts++
		case strings.Contains(name, "media/"): counts.Images++
		case strings.Contains(name, "comments"): counts.Comments++
		case strings.Contains(name, "persons"): counts.Persons++
		case name == "xl/vbaProject.bin": counts.Macros++
		}
		if strings.HasSuffix(name, ".rels") { counts.Relationships++ }
	}
	return counts
}

func inspectSheets(files map[string][]byte, sharedStrings int) []XLSXSheet {
	var sheets []XLSXSheet
	for name, body := range files {
		if !strings.HasPrefix(name, "xl/worksheets/sheet") || !strings.HasSuffix(name, ".xml") { continue }
		sheet := XLSXSheet{Path: name, SharedStrings: sharedStrings}
		decoder := xml.NewDecoder(strings.NewReader(string(body)))
		for { token, err := decoder.Token(); if err == io.EOF { break }; if err != nil { break }; start, ok := token.(xml.StartElement); if !ok { continue }
			switch start.Name.Local { case "c": sheet.Cells++; for _, attr := range start.Attr { if attr.Name.Local == "s" { sheet.StyledCells++ }; if attr.Name.Local == "t" && attr.Value == "s" { sheet.SharedStrings++ } }
			case "f": sheet.Formulas++; case "v": sheet.CachedValues++ }
		}
		sheets = append(sheets, sheet)
	}
	return sheets
}

func addFeatureWarnings(inspection *XLSXInspection) {
	if inspection.Features.Drawings > 0 { inspection.Warnings = append(inspection.Warnings, XLSXDiagnostic{Severity: "warning", Feature: "drawings", Message: "drawing resources require source preservation"}) }
	if inspection.Features.Comments > 0 || inspection.Features.Persons > 0 { inspection.Warnings = append(inspection.Warnings, XLSXDiagnostic{Severity: "warning", Feature: "comments", Message: "comment/person resources require source preservation"}) }
	if inspection.Features.Macros > 0 { inspection.Warnings = append(inspection.Warnings, XLSXDiagnostic{Severity: "warning", Feature: "macros", Message: "macros are never executed and require explicit preservation policy"}) }
}
