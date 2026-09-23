package csvx

import (
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

func invalidResult(diagnostic Diagnostic) ValidationResult {
	return ValidationResult{Valid: false, Errors: []Diagnostic{diagnostic}, Warnings: []Diagnostic{}}
}

func diagnosticForError(err error) Diagnostic {
	message := err.Error()
	code := "INVALID_PACKAGE"
	switch {
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
