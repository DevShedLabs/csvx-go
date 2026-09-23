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
	for i, name := range header {
		if name == "" {
			return nil, nil, fmt.Errorf("CSV header column %d is empty", i+1)
		}
	}
	for row, record := range records[1:] {
		if len(record) != len(header) {
			return nil, nil, fmt.Errorf("CSV row %d has %d fields; expected %d", row+2, len(record), len(header))
		}
	}
	return header, records[1:], nil
}

func writeCSV(w io.Writer, sheet *Sheet) error {
	writer := csv.NewWriter(w)
	header := make([]string, len(sheet.Columns))
	for i, column := range sheet.Columns {
		header[i] = column.Name
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, record := range sheet.Records {
		if len(record) != len(header) {
			return fmt.Errorf("sheet %q has a record with %d fields; expected %d", sheet.Name, len(record), len(header))
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("write CSV record: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	return nil
}
