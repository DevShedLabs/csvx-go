package csvx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Applies every csvx-spec/spec/15-edit-operations.md operation to each golden fixture in
// csvx-spec/examples/, writes the result as a real package, and validates it with the canonical
// validator (csvx-spec/validator) — csvx-spec/AGENTS.md rules 3.2 and 3.7. The conformance vectors
// compare models; only this proves the engine's edited output still conforms to schemas/*.json.
func TestEditedOutputConformsToSchemas(t *testing.T) {
	spec := specDir(t)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping csvx-spec/validator")
	}
	if _, err := os.Stat(filepath.Join(spec, "validator", "node_modules")); err != nil {
		t.Skip("csvx-spec/validator dependencies not installed")
	}
	dirs, _ := filepath.Glob(filepath.Join(spec, "examples", "*.csvx"))
	ran := 0
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		ran++
		t.Run(filepath.Base(dir), func(t *testing.T) {
			workbook, err := OpenDirectory(dir)
			if err != nil {
				t.Fatal(err)
			}
			first := workbook.Sheets[0].ID
			firstName := workbook.Sheets[0].Name
			// Fixtures declare no names, so add some and a validation rule that reads a range, to
			// cover rewriting of namedRanges and validation formulas as well.
			workbook.NamedRanges = []NamedRange{
				{Name: "FirstCell", RefersTo: "='" + firstName + "'!$A$2"},
				{Name: "Block", RefersTo: "='" + firstName + "'!$A$2:$A$5"},
				{Name: "Constant", RefersTo: "=6.28"},
			}
			var steps = []func(*Workbook) (*Workbook, error){
				func(w *Workbook) (*Workbook, error) { return SetCell(w, first, "A2", "x", EditOptions{}) },
				func(w *Workbook) (*Workbook, error) {
					return Paste(w, first, "A3", [][]string{{"=SUM(Block)"}, {"=FirstCell"}}, EditOptions{})
				},
				func(w *Workbook) (*Workbook, error) {
					next := cloneWorkbook(w)
					meta := next.Sheets[0].Cells["A2"]
					meta.Validation = []byte(`{"type":"list","formula1":"='` + firstName + `'!$A$2:$A$4"}`)
					next.Sheets[0].Cells["A2"] = meta
					return next, nil
				},
				func(w *Workbook) (*Workbook, error) { return InsertRows(w, first, 2, 2, EditOptions{}) },
				func(w *Workbook) (*Workbook, error) { return InsertColumns(w, first, "A", 1, EditOptions{}) },
				func(w *Workbook) (*Workbook, error) { return SetCell(w, first, "B2", "hello", EditOptions{}) },
				func(w *Workbook) (*Workbook, error) { return SetCell(w, first, "B3", "=B2", EditOptions{}) },
				func(w *Workbook) (*Workbook, error) {
					return Paste(w, first, "C2", [][]string{{"1", "2"}, {"3", "=C2+D2"}}, EditOptions{})
				},
				func(w *Workbook) (*Workbook, error) {
					return ApplyStyle(w, first, []string{"B2", "C2"}, map[string]any{"font": map[string]any{"bold": true}, "numberFormat": "0.00"}, EditOptions{})
				},
				func(w *Workbook) (*Workbook, error) {
					return ApplyStyle(w, first, []string{"B2"}, map[string]any{"fill": map[string]any{"color": "#ffff00"}}, EditOptions{})
				},
				func(w *Workbook) (*Workbook, error) { return ClearStyle(w, first, []string{"C2"}, EditOptions{}) },
				func(w *Workbook) (*Workbook, error) {
					return SetPrint(w, first, map[string]any{"orientation": "landscape", "scale": 90}, EditOptions{})
				},
				func(w *Workbook) (*Workbook, error) { return DeleteRows(w, first, []int{3}, EditOptions{}) },
				func(w *Workbook) (*Workbook, error) { return DeleteColumns(w, first, []string{"A"}, EditOptions{}) },
				func(w *Workbook) (*Workbook, error) { return AddSheet(w, EditOptions{}), nil },
				func(w *Workbook) (*Workbook, error) { return RenameSheet(w, first, "Renamed Sheet", EditOptions{}) },
			}
			for i, step := range steps {
				if workbook, err = step(workbook); err != nil {
					t.Fatalf("step %d: %v", i+1, err)
				}
			}
			if len(workbook.NamedRanges) != 3 {
				t.Fatalf("named ranges lost: %v", workbook.NamedRanges)
			}
			output := filepath.Join(t.TempDir(), "edited.csvx")
			if err := WritePackage(workbook, output); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Open(output)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalize(t, reloaded.NamedRanges), normalize(t, workbook.NamedRanges)) {
				t.Errorf("named ranges did not round-trip: %v vs %v", reloaded.NamedRanges, workbook.NamedRanges)
			}
			if out, err := exec.Command(node, filepath.Join(spec, "validator", "bin", "csvx-validate.mjs"), output).CombinedOutput(); err != nil {
				t.Fatalf("validator rejected edited package: %v\n%s", err, out)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no fixtures found")
	}
}
