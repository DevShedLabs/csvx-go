package csvx

import (
	"encoding/csv"
	"fmt"
	"io"
)

func readCSV(r io.Reader) ([]string, [][]string, error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = false
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read CSV: %w", err)
	}
	if len(records) == 0 || len(records[0]) == 0 {
		return nil, nil, fmt.Errorf("CSV must contain a header row")
	}

	header := records[0]
	for row, record := range records[1:] {
		if len(record) != len(header) {
			return nil, nil, fmt.Errorf("CSV row %d has %d fields; expected %d", row+2, len(record), len(header))
		}
	}
	return header, records[1:], nil
}

// writeCSV writes a sheet's header and records in the canonical form (spec 11.1, 11.2): LF
// terminators and minimal quoting.
func writeCSV(w io.Writer, sheet *Sheet) error {
	header := make([]string, len(sheet.Columns))
	for i, column := range sheet.Columns {
		header[i] = column.Name
	}
	rows := [][]string{header}
	for _, record := range sheet.Records {
		if len(record) != len(header) {
			return fmt.Errorf("sheet %q has a record with %d fields; expected %d", sheet.Name, len(record), len(header))
		}
		rows = append(rows, record)
	}
	if _, err := io.WriteString(w, formatCSV(rows, ',')); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}
