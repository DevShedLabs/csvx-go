package csvx

import (
	"bytes"
	"os"
	"strings"
)

// Diagnostic describes a validation error or warning at a package resource.
type Diagnostic struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// ValidationResult contains machine-readable package validation output.
type ValidationResult struct {
	Valid    bool         `json:"valid"`
	Errors   []Diagnostic `json:"errors"`
	Warnings []Diagnostic `json:"warnings"`
}

// Validate checks a CSVX ZIP file or unpacked CSVX directory.
func Validate(filename string) ValidationResult {
	info, err := os.Stat(filename)
	if err != nil {
		return invalidResult(diagnosticForError(err))
	}
	if info.IsDir() {
		_, err = OpenDirectory(filename)
	} else {
		_, err = Open(filename)
	}
	if err != nil {
		return invalidResult(diagnosticForError(err))
	}
	return ValidationResult{Valid: true, Errors: []Diagnostic{}, Warnings: []Diagnostic{}}
}

// ValidateWorkbook validates a workbook that exists only in memory — for example one that has been
// edited but not yet saved — by serializing it exactly as WritePackage would and loading the result
// back, so what is checked is what would be written. Like Validate, this is a structural check (the
// package loads, resources decode, named ranges are valid); JSON-Schema conformance has one home,
// csvx-spec/validator (csvx-spec/AGENTS.md rule 3.3), which a host can run on the saved package.
func ValidateWorkbook(workbook *Workbook) ValidationResult {
	var buffer bytes.Buffer
	if err := WritePackageTo(workbook, &buffer); err != nil {
		return invalidResult(diagnosticForError(err))
	}
	if _, err := Load(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); err != nil {
		return invalidResult(diagnosticForError(err))
	}
	return ValidationResult{Valid: true, Errors: []Diagnostic{}, Warnings: []Diagnostic{}}
}

func invalidResult(diagnostic Diagnostic) ValidationResult {
	return ValidationResult{Valid: false, Errors: []Diagnostic{diagnostic}, Warnings: []Diagnostic{}}
}

func diagnosticForError(err error) Diagnostic {
	message := err.Error()
	code := "INVALID_PACKAGE"
	switch {
	case containsAny(message, `missing package entry "manifest.json"`):
		code = "MISSING_MANIFEST"
	case containsAny(message, "COLUMN_NAME_MISMATCH"):
		code = "COLUMN_NAME_MISMATCH"
	case containsAny(message, "DUPLICATE_SHEET_NAME"):
		code = "DUPLICATE_SHEET_NAME"
	case containsAny(message, "INVALID_NAMED_RANGE"):
		code = "INVALID_NAMED_RANGE"
	case containsAny(message, "missing package entry", "missing manifest", "missing workbook"):
		code = "MISSING_RESOURCE"
	case containsAny(message, "decode", "malformed"):
		code = "INVALID_JSON"
	case containsAny(message, "CSV", "csv"):
		code = "INVALID_CSV"
	case containsAny(message, "duplicate ZIP entry"):
		code = "DUPLICATE_ENTRY"
	case containsAny(message, "unsafe", "invalid ZIP entry path"):
		code = "UNSAFE_ENTRY"
	}
	return Diagnostic{Code: code, Message: message, Severity: "error"}
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
