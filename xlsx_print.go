package csvx

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/DevShedLabs/csvx-go/internal/schema"
)

type xlsxPageSetup struct {
	Orientation string `xml:"orientation,attr"`
	PaperSize   string `xml:"paperSize,attr"`
	Scale       string `xml:"scale,attr"`
	FitToWidth  string `xml:"fitToWidth,attr"`
	FitToHeight string `xml:"fitToHeight,attr"`
	PageOrder   string `xml:"pageOrder,attr"`
}

type xlsxPrint struct {
	SheetPr struct {
		PageSetUpPr struct {
			FitToPage string `xml:"fitToPage,attr"`
		} `xml:"pageSetUpPr"`
	} `xml:"sheetPr"`
	PrintOptions *struct {
		GridLines          string `xml:"gridLines,attr"`
		HorizontalCentered string `xml:"horizontalCentered,attr"`
	} `xml:"printOptions"`
	PageMargins *struct {
		Left   string `xml:"left,attr"`
		Right  string `xml:"right,attr"`
		Top    string `xml:"top,attr"`
		Bottom string `xml:"bottom,attr"`
	} `xml:"pageMargins"`
	PageSetup *xlsxPageSetup `xml:"pageSetup"`
	RowBreaks []struct {
		ID string `xml:"id,attr"`
	} `xml:"rowBreaks>brk"`
	ColBreaks []struct {
		ID string `xml:"id,attr"`
	} `xml:"colBreaks>brk"`
}

type xlsxDefinedName struct {
	Name         string `xml:"name,attr"`
	LocalSheetID string `xml:"localSheetId,attr"`
	Value        string `xml:",chardata"`
}

// Spec defaults (spec/03-sheets.md): margins equal to these, and a scale of 100, carry no
// information and are omitted on import.
const defaultMarginTopBottom, defaultMarginLeftRight = 0.75, 0.7

var xlsxPaperSizes = map[string]schema.PrintPaperSize{
	"1": schema.PrintPaperSizeLetter, "3": schema.PrintPaperSizeTabloid, "5": schema.PrintPaperSizeLegal,
	"8": schema.PrintPaperSizeA3, "9": schema.PrintPaperSizeA4, "11": schema.PrintPaperSizeA5,
}

func xlsxBool(value string) bool { return value == "1" || strings.EqualFold(value, "true") }

// printFromWorksheet maps a worksheet's page-setup elements onto CSVX print settings
// (spec/14-xlsx-interoperability.md, 14.6). Returns nil when the sheet states nothing meaningful.
func printFromWorksheet(source xlsxPrint) *PrintSettings {
	print := schema.Print{}
	extra := map[string]any{}
	if setup := source.PageSetup; setup != nil {
		switch setup.Orientation {
		case "portrait":
			value := schema.PrintOrientationPortrait
			print.Orientation = &value
		case "landscape":
			value := schema.PrintOrientationLandscape
			print.Orientation = &value
		}
		if setup.PaperSize != "" {
			if size, ok := xlsxPaperSizes[setup.PaperSize]; ok {
				print.PaperSize = &size
			} else if id, err := strconv.Atoi(setup.PaperSize); err == nil {
				extra["xlsxPaperSize"] = id
			}
		}
		switch setup.PageOrder {
		case "downThenOver":
			value := schema.PrintPageOrderDownThenOver
			print.PageOrder = &value
		case "overThenDown":
			value := schema.PrintPageOrderOverThenDown
			print.PageOrder = &value
		}
		if scale, err := strconv.ParseFloat(setup.Scale, 64); err == nil && scale != 100 && !xlsxBool(source.SheetPr.PageSetUpPr.FitToPage) {
			print.Scale = &scale
		}
		if xlsxBool(source.SheetPr.PageSetUpPr.FitToPage) {
			// XLSX defaults both to 1 page when fit-to-page is on and the attribute is absent.
			width, height := 1, 1
			if value, err := strconv.Atoi(setup.FitToWidth); err == nil {
				width = value
			}
			if value, err := strconv.Atoi(setup.FitToHeight); err == nil {
				height = value
			}
			print.FitToWidth, print.FitToHeight = &width, &height
		}
	} else if xlsxBool(source.SheetPr.PageSetUpPr.FitToPage) {
		width, height := 1, 1
		print.FitToWidth, print.FitToHeight = &width, &height
	}
	if options := source.PrintOptions; options != nil {
		if xlsxBool(options.GridLines) {
			value := true
			print.Gridlines = &value
		}
		if xlsxBool(options.HorizontalCentered) {
			value := true
			print.CenterHorizontally = &value
		}
	}
	if margins := source.PageMargins; margins != nil {
		top, _ := strconv.ParseFloat(margins.Top, 64)
		right, _ := strconv.ParseFloat(margins.Right, 64)
		bottom, _ := strconv.ParseFloat(margins.Bottom, 64)
		left, _ := strconv.ParseFloat(margins.Left, 64)
		isDefault := top == defaultMarginTopBottom && bottom == defaultMarginTopBottom && left == defaultMarginLeftRight && right == defaultMarginLeftRight
		if !isDefault && margins.Top != "" {
			print.Margins = &schema.PrintMargins{Top: &top, Right: &right, Bottom: &bottom, Left: &left}
		}
	}
	for _, brk := range source.ColBreaks {
		if id, err := strconv.Atoi(brk.ID); err == nil && id > 0 {
			print.ColumnBreaks = append(print.ColumnBreaks, id)
		}
	}
	for _, brk := range source.RowBreaks {
		if id, err := strconv.Atoi(brk.ID); err == nil && id > 0 {
			print.RowBreaks = append(print.RowBreaks, id)
		}
	}
	if len(extra) > 0 {
		print.AdditionalProperties = extra
	}
	return nonEmptyPrint(print)
}

func nonEmptyPrint(print schema.Print) *PrintSettings {
	if print.Orientation == nil && print.PaperSize == nil && print.Margins == nil && print.Scale == nil &&
		print.FitToWidth == nil && print.FitToHeight == nil && print.Area == nil && print.RepeatRows == nil &&
		print.RepeatColumns == nil && print.PageOrder == nil && print.Gridlines == nil &&
		print.CenterHorizontally == nil && len(print.ColumnBreaks) == 0 && len(print.RowBreaks) == 0 &&
		print.AdditionalProperties == nil {
		return nil
	}
	settings := PrintSettings(print)
	return &settings
}

var (
	cellRangePattern   = regexp.MustCompile(`^\$?([A-Z]+)\$?([0-9]+)(?::\$?([A-Z]+)\$?([0-9]+))?$`)
	rowRangePattern    = regexp.MustCompile(`^\$?([0-9]+):\$?([0-9]+)$`)
	columnRangePattern = regexp.MustCompile(`^\$?([A-Z]+):\$?([A-Z]+)$`)
)

// applyXLSXPrintNames applies the sheet-scoped _xlnm.Print_Area and _xlnm.Print_Titles defined
// names to a sheet's print settings, creating them if the worksheet itself stated none.
func applyXLSXPrintNames(sheet *Sheet, sheetIndex int, names []xlsxDefinedName) {
	for _, name := range names {
		if name.LocalSheetID != strconv.Itoa(sheetIndex) || (name.Name != "_xlnm.Print_Area" && name.Name != "_xlnm.Print_Titles") {
			continue
		}
		if sheet.Print == nil {
			sheet.Print = &PrintSettings{}
		}
		parts := strings.Split(strings.TrimSpace(name.Value), ",")
		for _, part := range parts {
			reference := part
			if bang := strings.LastIndex(part, "!"); bang >= 0 {
				reference = part[bang+1:]
			}
			switch name.Name {
			case "_xlnm.Print_Area":
				// Only a single contiguous area maps; a multi-area print range is kept verbatim.
				match := cellRangePattern.FindStringSubmatch(reference)
				if match == nil || len(parts) > 1 {
					sheet.Print.AdditionalProperties = withExtra(sheet.Print.AdditionalProperties, "xlsxPrintArea", name.Value)
					break
				}
				area := match[1] + match[2] + ":" + match[1] + match[2]
				if match[3] != "" {
					area = match[1] + match[2] + ":" + match[3] + match[4]
				}
				sheet.Print.Area = &area
			case "_xlnm.Print_Titles":
				if match := rowRangePattern.FindStringSubmatch(reference); match != nil {
					rows := match[1] + ":" + match[2]
					sheet.Print.RepeatRows = &rows
				} else if match := columnRangePattern.FindStringSubmatch(reference); match != nil {
					columns := match[1] + ":" + match[2]
					sheet.Print.RepeatColumns = &columns
				}
			}
		}
	}
}

func withExtra(existing any, key string, value any) map[string]any {
	extra, _ := existing.(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	extra[key] = value
	return extra
}
