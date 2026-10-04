package csvx

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// Runs csvx-spec/tests/print/pagination.json verbatim (operation "paginate") — csvx-spec/AGENTS.md
// rule 3.4. The vector describes a sheet compactly (column widths, a record count, sparse text
// values); this materializes it and compares the pages, scale, area, and printable size.
func TestPaginationVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(specDir(t, "tests", "print"), "pagination.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Cases []struct {
			Note  string `json:"note"`
			Input struct {
				Sheet struct {
					Columns     []Column                `json:"columns"`
					RecordCount int                     `json:"recordCount"`
					Values      map[string]string       `json:"values"`
					RowHeights  map[string]float64      `json:"rowHeights"`
					Cells       map[string]CellMetadata `json:"cells"`
				} `json:"sheet"`
				Print  json.RawMessage `json:"print"`
				Styles []Style         `json:"styles"`
			} `json:"input"`
			Expected struct {
				Pages []struct {
					Rows    []int
					Columns []string
				} `json:"pages"`
				Scale     *float64                         `json:"scale"`
				Area      *string                          `json:"area"`
				Printable *struct{ Width, Height float64 } `json:"printable"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	for i, c := range vector.Cases {
		name := "#" + strconv.Itoa(i+1) + ": " + c.Note
		sheet := &Sheet{ID: "sheet-1", Name: "Sheet1", Columns: c.Input.Sheet.Columns, Cells: c.Input.Sheet.Cells, RowHeights: map[int]float64{}}
		for k, v := range c.Input.Sheet.RowHeights {
			n, _ := strconv.Atoi(k)
			sheet.RowHeights[n] = v
		}
		for r := 0; r < c.Input.Sheet.RecordCount; r++ {
			sheet.Records = append(sheet.Records, make([]string, len(sheet.Columns)))
		}
		for coordinate, text := range c.Input.Sheet.Values {
			if column, row, ok := IndicesForCoordinate(coordinate); ok && row >= 0 {
				sheet.Records[row][column] = text
			}
		}
		var print PrintSettings
		if err := json.Unmarshal(c.Input.Print, &print); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sheet.Print = &print

		got := Paginate(sheet, c.Input.Styles)
		if c.Expected.Pages != nil {
			var pages []struct {
				Rows    []int
				Columns []string
			}
			for _, p := range got.Pages {
				pages = append(pages, struct {
					Rows    []int
					Columns []string
				}{p.Rows, p.Columns})
			}
			want := make([]struct {
				Rows    []int
				Columns []string
			}, len(c.Expected.Pages))
			for j, p := range c.Expected.Pages {
				want[j].Rows, want[j].Columns = p.Rows, p.Columns
			}
			if !reflect.DeepEqual(pages, want) {
				t.Errorf("%s: pages differ\n got %v\nwant %v", name, pages, want)
			}
		}
		if c.Expected.Scale != nil && math.Abs(got.Scale-*c.Expected.Scale) > 1e-9 {
			t.Errorf("%s: scale = %v; want %v", name, got.Scale, *c.Expected.Scale)
		}
		if c.Expected.Area != nil && got.Area != *c.Expected.Area {
			t.Errorf("%s: area = %q; want %q", name, got.Area, *c.Expected.Area)
		}
		if p := c.Expected.Printable; p != nil && (math.Abs(got.PrintableWidth-p.Width) > 1e-6 || math.Abs(got.PrintableHeight-p.Height) > 1e-6) {
			t.Errorf("%s: printable = %v x %v; want %v x %v", name, got.PrintableWidth, got.PrintableHeight, p.Width, p.Height)
		}
		for j, p := range got.Pages {
			if p.Number != j+1 {
				t.Errorf("%s: page %d numbered %d", name, j+1, p.Number)
			}
		}
	}
}
