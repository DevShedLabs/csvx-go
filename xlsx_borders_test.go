package csvx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const bordersFixture = "../csvx-spec/examples/borders.xlsx"

func importBordersFixture(t *testing.T) *Workbook {
	t.Helper()
	inspection, err := InspectXLSX(bordersFixture)
	if err != nil {
		t.Fatal(err)
	}
	workbook, err := importXLSXWorkbook(bordersFixture, inspection)
	if err != nil {
		t.Fatal(err)
	}
	return workbook
}

func styleOfCell(t *testing.T, workbook *Workbook, coordinate string) Style {
	t.Helper()
	id := workbook.Sheets[0].Cells[coordinate].Style
	for _, style := range workbook.Styles {
		if style.Id == id {
			return style
		}
	}
	t.Fatalf("cell %s has no style (id %q)", coordinate, id)
	return Style{}
}

func TestXLSXImportMapsBordersPerEdge(t *testing.T) {
	workbook := importBordersFixture(t)

	a1 := styleOfCell(t, workbook, "A1").Border
	if a1 == nil || a1.Bottom == nil || *a1.Bottom.Style != "thin" || *a1.Bottom.Color != "#000000" {
		t.Fatalf("A1 bottom edge wrong: %+v", a1)
	}
	if a1.Top != nil || a1.Left != nil || a1.Right != nil {
		t.Fatalf("A1 must only declare a bottom edge: %+v", a1)
	}

	b1 := styleOfCell(t, workbook, "B1").Border
	for name, edge := range map[string]any{"top": b1.Top, "right": b1.Right, "bottom": b1.Bottom, "left": b1.Left} {
		if edge == nil {
			t.Fatalf("B1 %s edge missing", name)
		}
	}
	if *b1.Left.Style != "medium" || *b1.Left.Color != "#FF0000" {
		t.Fatalf("B1 left edge wrong: %+v", b1.Left)
	}

	// "hair" has no spec counterpart: mapped to thin, original kept so nothing is silently lost.
	c1 := styleOfCell(t, workbook, "C1").Border
	if c1 == nil || c1.Left == nil || *c1.Left.Style != "thin" {
		t.Fatalf("C1 left edge wrong: %+v", c1)
	}
	extra, _ := c1.Left.AdditionalProperties.(map[string]any)
	if extra["xlsxStyle"] != "hair" {
		t.Fatalf("C1 should preserve original line style, got %#v", c1.Left.AdditionalProperties)
	}

	if id := workbook.Sheets[0].Cells["E1"].Style; id != "" {
		if border := styleOfCell(t, workbook, "E1").Border; border != nil {
			t.Fatalf("E1 declares no border, got %+v", border)
		}
	}
}

// The written package must satisfy styles.schema.json, checked with the spec's reference validator.
func TestXLSXImportBordersSchemaValid(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; cannot run csvx-spec/validator")
	}
	workbook := importBordersFixture(t)
	output := filepath.Join(t.TempDir(), "borders.csvx")
	if err := WritePackage(workbook, output); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Command(node, "../csvx-spec/validator/bin/csvx-validate.mjs", output).CombinedOutput()
	if err != nil {
		t.Fatalf("validator rejected package: %v\n%s", err, result)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}
