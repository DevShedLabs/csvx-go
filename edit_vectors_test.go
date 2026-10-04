package csvx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// Runs csvx-spec/tests/edit/*.json (except text-case, which has its own runner) verbatim against
// this engine (csvx-spec/AGENTS.md rule 3.4). Operations run with recalculation off because the
// vectors compare the model as the operation leaves it (spec/15-edit-operations.md).

func runEditOperation(operation string, workbook *Workbook, a map[string]any) (*Workbook, error) {
	off := EditOptions{SkipRecalculate: true}
	str := func(key string) string { s, _ := a[key].(string); return s }
	num := func(key string) int { n, _ := a[key].(float64); return int(n) }
	strs := func(key string) []string {
		var out []string
		for _, v := range a[key].([]any) {
			out = append(out, v.(string))
		}
		return out
	}
	switch operation {
	case "insert-rows":
		return InsertRows(workbook, str("sheet"), num("at"), num("count"), off)
	case "delete-rows":
		var rows []int
		for _, v := range a["rows"].([]any) {
			rows = append(rows, int(v.(float64)))
		}
		return DeleteRows(workbook, str("sheet"), rows, off)
	case "insert-columns":
		return InsertColumns(workbook, str("sheet"), str("at"), num("count"), off)
	case "delete-columns":
		return DeleteColumns(workbook, str("sheet"), strs("columns"), off)
	case "add-sheet":
		return AddSheet(workbook, off), nil
	case "rename-sheet":
		return RenameSheet(workbook, str("sheet"), str("name"), off)
	case "delete-sheet":
		return DeleteSheet(workbook, str("sheet"), off)
	case "set-cell":
		return SetCell(workbook, str("sheet"), str("coordinate"), str("text"), off)
	case "paste":
		var rows [][]string
		for _, row := range a["rows"].([]any) {
			var texts []string
			for _, v := range row.([]any) {
				texts = append(texts, v.(string))
			}
			rows = append(rows, texts)
		}
		return Paste(workbook, str("sheet"), str("anchor"), rows, off)
	case "apply-style":
		return ApplyStyle(workbook, str("sheet"), strs("coordinates"), a["patch"].(map[string]any), off)
	case "clear-style":
		return ClearStyle(workbook, str("sheet"), strs("coordinates"), off)
	case "set-print":
		return SetPrint(workbook, str("sheet"), a["patch"].(map[string]any), off)
	}
	return nil, errors.New("runner does not know operation " + operation)
}

func formulasOf(sheet *Sheet) map[string]any {
	out := map[string]any{}
	for coordinate, metadata := range sheet.Cells {
		if metadata.Formula != "" {
			out[coordinate] = metadata.Formula
		}
	}
	return out
}

func TestEditVectors(t *testing.T) {
	dir := specDir(t, "tests", "edit")
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(paths)
	ran := 0
	for _, path := range paths {
		if filepath.Base(path) == "text-case.json" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var vector struct {
			ID        string `json:"id"`
			Operation string `json:"operation"`
			Cases     []struct {
				Note      string `json:"note"`
				Operation string `json:"operation"`
				Input     struct {
					Workbook json.RawMessage `json:"workbook"`
					Args     map[string]any  `json:"args"`
				} `json:"input"`
				Expected map[string]any `json:"expected"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(raw, &vector); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for i, c := range vector.Cases {
			ran++
			name := vector.ID + " #" + string(rune('0'+i+1)) + ": " + c.Note
			operation := c.Operation
			if operation == "" {
				operation = vector.Operation
			}
			var workbook Workbook
			if err := json.Unmarshal(c.Input.Workbook, &workbook); err != nil {
				t.Fatalf("%s: bad workbook: %v", name, err)
			}
			for _, sheet := range workbook.Sheets {
				if sheet.Path == "" {
					sheet.Path = "sheets/" + sheet.ID + ".csv"
				}
			}
			before := normalize(t, workbook)
			result, err := runEditOperation(operation, &workbook, c.Input.Args)
			var invalid *InvalidEditError
			if valid, ok := c.Expected["valid"].(bool); ok && !valid {
				if !errors.As(err, &invalid) {
					t.Errorf("%s: want InvalidEditError, got %v", name, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if !reflect.DeepEqual(before, normalize(t, workbook)) {
				t.Errorf("%s: operation mutated its input", name)
			}
			checkEditResult(t, name, operation, result, c.Input.Args, c.Expected)
		}
	}
	if ran == 0 {
		t.Fatal("no edit vectors ran")
	}
}

func checkEditResult(t *testing.T, name, operation string, result *Workbook, args, e map[string]any) {
	t.Helper()
	target := result.Sheets[0]
	if operation == "add-sheet" {
		target = result.Sheets[len(result.Sheets)-1]
	} else {
		for _, s := range result.Sheets {
			if s.ID == args["sheet"] || s.Name == args["sheet"] {
				target = s
				break
			}
		}
	}
	byID := func(id string) *Sheet {
		for _, s := range result.Sheets {
			if s.ID == id {
				return s
			}
		}
		return nil
	}
	compare := func(what string, got, want any) {
		if !reflect.DeepEqual(normalize(t, got), want) {
			t.Errorf("%s: %s = %v; want %v", name, what, normalize(t, got), want)
		}
	}
	perSheet := func(what string, value any, pick func(*Sheet) any) {
		if keyed, ok := value.(map[string]any); ok {
			all := len(keyed) > 0
			for id := range keyed {
				if byID(id) == nil {
					all = false
				}
			}
			if all {
				for id, want := range keyed {
					compare(what+"["+id+"]", pick(byID(id)), want)
				}
				return
			}
		}
		compare(what, pick(target), value)
	}
	if v, ok := e["formulas"]; ok {
		perSheet("formulas", v, func(s *Sheet) any { return formulasOf(s) })
	}
	if v, ok := e["columns"]; ok {
		perSheet("columns", v, func(s *Sheet) any { return s.Columns })
	}
	if v, ok := e["records"]; ok {
		records := target.Records
		if records == nil {
			records = [][]string{}
		}
		compare("records", records, v)
	}
	if v, ok := e["cells"]; ok {
		cells := target.Cells
		if cells == nil {
			cells = map[string]CellMetadata{}
		}
		compare("cells", cells, v)
	}
	if v, ok := e["namedRanges"]; ok {
		compare("namedRanges", result.NamedRanges, v)
	}
	if v, ok := e["validationFormulas"]; ok {
		perSheet("validationFormulas", v, func(s *Sheet) any {
			out := map[string]any{}
			for coordinate, metadata := range s.Cells {
				var rule map[string]any
				if len(metadata.Validation) > 0 && json.Unmarshal(metadata.Validation, &rule) == nil {
					for _, key := range []string{"formula1", "formula2"} {
						if text, ok := rule[key].(string); ok {
							out[coordinate] = text
							break
						}
					}
				}
			}
			return out
		})
	}
	if v, ok := e["styles"]; ok {
		styles := result.Styles
		if styles == nil {
			styles = []Style{}
		}
		compare("styles", styles, v)
	}
	if v, ok := e["print"]; ok {
		var got any
		if target.Print != nil {
			got = target.Print
		}
		compare("print", got, v)
	}
	if v, ok := e["names"]; ok {
		var names []string
		for _, s := range result.Sheets {
			names = append(names, s.Name)
		}
		compare("names", names, v)
	}
	if v, ok := e["newSheet"].(map[string]any); ok {
		compare("newSheet.columns", target.Columns, v["columns"])
		records := target.Records
		if records == nil {
			records = [][]string{}
		}
		compare("newSheet.records", records, v["records"])
	}
	unique := func(keys func(*Sheet) string) bool {
		seen := map[string]bool{}
		for _, s := range result.Sheets {
			if seen[keys(s)] {
				return false
			}
			seen[keys(s)] = true
		}
		return true
	}
	if e["idUnique"] == true && !unique(func(s *Sheet) string { return s.ID }) {
		t.Errorf("%s: sheet ids not unique", name)
	}
	if e["nameUnique"] == true && !unique(func(s *Sheet) string { return s.Name }) {
		t.Errorf("%s: sheet names not unique", name)
	}
}
