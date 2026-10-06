package csvx

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/DevShedLabs/csvx-go/internal/schema"
)

type xlsxStyles struct {
	NumFmts struct {
		Items []struct {
			ID   string `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmt"`
	} `xml:"numFmts"`
	Fonts struct {
		Items []xlsxFont `xml:"font"`
	} `xml:"fonts"`
	Fills struct {
		Items []xlsxFill `xml:"fill"`
	} `xml:"fills"`
	Borders struct {
		Items []xlsxBorder `xml:"border"`
	} `xml:"borders"`
	CellXfs struct {
		Items []xlsxXF `xml:"xf"`
	} `xml:"cellXfs"`
}
type xlsxXF struct {
	NumFmtID          string         `xml:"numFmtId,attr"`
	FontID            string         `xml:"fontId,attr"`
	FillID            string         `xml:"fillId,attr"`
	BorderID          string         `xml:"borderId,attr"`
	ApplyNumberFormat string         `xml:"applyNumberFormat,attr"`
	Alignment         *xlsxAlignment `xml:"alignment"`
}
type xlsxAlignment struct {
	Horizontal   string `xml:"horizontal,attr"`
	Vertical     string `xml:"vertical,attr"`
	WrapText     string `xml:"wrapText,attr"`
	TextRotation string `xml:"textRotation,attr"`
	Indent       string `xml:"indent,attr"`
}
type xlsxFont struct {
	Name []struct {
		Value string `xml:"val,attr"`
	} `xml:"name"`
	Size []struct {
		Value string `xml:"val,attr"`
	} `xml:"sz"`
	Bold   []struct{}  `xml:"b"`
	Italic []struct{}  `xml:"i"`
	Color  []xlsxColor `xml:"color"`
}
type xlsxFill struct {
	Pattern []struct {
		Type       string      `xml:"patternType,attr"`
		Foreground []xlsxColor `xml:"fgColor"`
	} `xml:"patternFill"`
}
type xlsxBorder struct {
	Left   xlsxBorderEdge `xml:"left"`
	Right  xlsxBorderEdge `xml:"right"`
	Top    xlsxBorderEdge `xml:"top"`
	Bottom xlsxBorderEdge `xml:"bottom"`
}
type xlsxBorderEdge struct {
	Style string      `xml:"style,attr"`
	Color []xlsxColor `xml:"color"`
}
type xlsxColor struct {
	RGB     string `xml:"rgb,attr"`
	Theme   string `xml:"theme,attr"`
	Indexed string `xml:"indexed,attr"`
}

func parseXLSXStyles(body []byte) (map[string]map[string]any, error) {
	styles := map[string]map[string]any{}
	if len(body) == 0 {
		return styles, nil
	}
	var resource xlsxStyles
	if err := xml.Unmarshal(body, &resource); err != nil {
		return nil, fmt.Errorf("decode XLSX styles: %w", err)
	}
	formats := map[string]string{}
	for _, item := range resource.NumFmts.Items {
		formats[item.ID] = item.Code
	}
	for index, xf := range resource.CellXfs.Items {
		style := map[string]any{}
		if value, ok := formats[xf.NumFmtID]; ok {
			style["numberFormat"] = value
		} else if code := builtinNumberFormat(xf.NumFmtID); code != "" {
			style["numberFormat"] = code
		}
		if font, ok := indexedFont(resource.Fonts.Items, xf.FontID); ok {
			style["font"] = font
		}
		if fill, ok := indexedFill(resource.Fills.Items, xf.FillID); ok {
			style["fill"] = fill
		}
		if border, ok := indexedBorder(resource.Borders.Items, xf.BorderID); ok {
			style["border"] = border
		}
		if xf.Alignment != nil {
			style["alignment"] = map[string]any{"horizontal": xf.Alignment.Horizontal, "vertical": xf.Alignment.Vertical, "wrapText": xf.Alignment.WrapText == "1" || strings.EqualFold(xf.Alignment.WrapText, "true"), "textRotation": xf.Alignment.TextRotation, "indent": xf.Alignment.Indent}
		}
		styles[strconv.Itoa(index)] = style
	}
	return styles, nil
}

// styleIDPrefix keeps generated style ids conformant with styles.schema.json's id pattern
// (must start with a letter). The XLSX cellXfs index is numeric on its own and is not valid here.
const styleIDPrefix = "s"

// exportStyles converts the raw numeric-indexed style map into the schema's canonical shape: an
// array of self-identifying style records with stable, schema-conformant ids.
func exportStyles(raw map[string]map[string]any) []Style {
	indices := make([]int, 0, len(raw))
	for key := range raw {
		if index, err := strconv.Atoi(key); err == nil {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	styles := make([]Style, 0, len(indices))
	for _, index := range indices {
		styles = append(styles, styleFromMap(styleIDPrefix+strconv.Itoa(index), raw[strconv.Itoa(index)]))
	}
	return styles
}

func styleFromMap(id string, source map[string]any) Style {
	style := Style{Id: id}
	if value, ok := source["numberFormat"].(string); ok {
		style.NumberFormat = &value
	}
	if value, ok := source["font"].(map[string]any); ok {
		// Through the generated type, so size is a number and an unknown key is kept (spec/08-styles.md).
		if body, err := json.Marshal(value); err == nil {
			var font schema.CSVXStylesStylesElemFont
			if json.Unmarshal(body, &font) == nil {
				style.Font = &font
			}
		}
	}
	if value, ok := source["fill"].(map[string]any); ok {
		style.Fill = value
	}
	if value, ok := source["border"].(schema.CSVXStylesStylesElemBorder); ok {
		style.Border = &value
	}
	if value, ok := source["alignment"].(map[string]any); ok {
		style.Alignment = value
	}
	return style
}

// xlsxStyleRef converts a raw XLSX cellXfs index (as found on a cell's `s` attribute) into the
// schema-conformant style id used in exported Style records and cell metadata `style` references.
func xlsxStyleRef(rawIndex string) string {
	if rawIndex == "" {
		return ""
	}
	return styleIDPrefix + rawIndex
}

func indexedFont(fonts []xlsxFont, id string) (map[string]any, bool) {
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 || index >= len(fonts) {
		return nil, false
	}
	source := fonts[index]
	font := map[string]any{}
	if len(source.Name) > 0 {
		font["name"] = source.Name[0].Value
	}
	if len(source.Size) > 0 {
		// spec/08-styles.md: size is a number of points, never a string.
		if size, err := strconv.ParseFloat(source.Size[0].Value, 64); err == nil && size > 0 {
			font["size"] = size
		}
	}
	if len(source.Bold) > 0 {
		font["bold"] = true
	}
	if len(source.Italic) > 0 {
		font["italic"] = true
	}
	if color := firstColor(source.Color); color != "" {
		font["color"] = color
	}
	return font, true
}

// xlsxBorderStyles maps XLSX border line names onto the spec's border line styles
// (spec/08-styles.md). Names with no exact counterpart map to the nearest one; the original name is
// then kept on the edge as `xlsxStyle` so nothing is silently lost.
var xlsxBorderStyles = map[string]schema.BorderLineStyle{
	"thin": schema.BorderLineStyleThin, "hair": schema.BorderLineStyleThin,
	"medium": schema.BorderLineStyleMedium, "thick": schema.BorderLineStyleThick,
	"dashed": schema.BorderLineStyleDashed, "mediumDashed": schema.BorderLineStyleDashed,
	"dashDot": schema.BorderLineStyleDashed, "mediumDashDot": schema.BorderLineStyleDashed,
	"dashDotDot": schema.BorderLineStyleDashed, "mediumDashDotDot": schema.BorderLineStyleDashed,
	"slantDashDot": schema.BorderLineStyleDashed,
	"dotted":       schema.BorderLineStyleDotted, "double": schema.BorderLineStyleDouble,
}

func borderEdge(edge xlsxBorderEdge) *schema.BorderEdge {
	if edge.Style == "" || edge.Style == "none" {
		return nil
	}
	style, ok := xlsxBorderStyles[edge.Style]
	if !ok {
		style = schema.BorderLineStyleThin
	}
	result := &schema.BorderEdge{Style: &style}
	if color := firstColor(edge.Color); color != "" {
		result.Color = &color
	}
	if exact := string(style) == edge.Style; !exact {
		result.AdditionalProperties = map[string]any{"xlsxStyle": edge.Style}
	}
	return result
}

func indexedBorder(borders []xlsxBorder, id string) (schema.CSVXStylesStylesElemBorder, bool) {
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 || index >= len(borders) {
		return schema.CSVXStylesStylesElemBorder{}, false
	}
	source := borders[index]
	border := schema.CSVXStylesStylesElemBorder{
		Top: borderEdge(source.Top), Right: borderEdge(source.Right),
		Bottom: borderEdge(source.Bottom), Left: borderEdge(source.Left),
	}
	if border.Top == nil && border.Right == nil && border.Bottom == nil && border.Left == nil {
		return border, false
	}
	return border, true
}
func indexedFill(fills []xlsxFill, id string) (map[string]any, bool) {
	index, err := strconv.Atoi(id)
	if err != nil || index < 0 || index >= len(fills) || len(fills[index].Pattern) == 0 {
		return nil, false
	}
	pattern := fills[index].Pattern[0]
	fill := map[string]any{"pattern": pattern.Type}
	if color := firstColor(pattern.Foreground); color != "" {
		fill["color"] = color
	}
	return fill, true
}
func firstColor(colors []xlsxColor) string {
	if len(colors) == 0 {
		return ""
	}
	color := colors[0]
	if color.RGB != "" {
		rgb := strings.TrimPrefix(strings.ToUpper(color.RGB), "FF")
		if len(rgb) == 6 {
			return "#" + rgb
		}
	}
	if color.Theme != "" {
		return themeColor(color.Theme)
	}
	if color.Indexed != "" {
		return indexedColor(color.Indexed)
	}
	return ""
}
func themeColor(theme string) string {
	colors := map[string]string{"0": "#FFFFFF", "1": "#000000", "2": "#E7E6E6", "3": "#44546A", "4": "#4472C4", "5": "#ED7D31", "6": "#A5A5A5", "7": "#FFC000", "8": "#5B9BD5", "9": "#70AD47"}
	return colors[theme]
}
func indexedColor(index string) string {
	colors := map[string]string{"0": "#000000", "1": "#FFFFFF", "2": "#FF0000", "3": "#00FF00", "4": "#0000FF", "5": "#FFFF00", "6": "#FF00FF", "7": "#00FFFF", "8": "#000000", "9": "#FFFFFF", "10": "#FF0000", "11": "#00FF00", "12": "#0000FF", "13": "#FFFF00", "14": "#FF00FF", "15": "#00FFFF"}
	return colors[index]
}
func builtinNumberFormat(id string) string {
	switch id {
	case "0":
		return "0"
	case "1":
		return "0"
	case "2":
		return "0.00"
	case "4":
		return "#,##0.00"
	case "9":
		return "0%"
	case "10":
		return "0.00%"
	case "14":
		return "m/d/yy"
	case "20":
		return "h:mm"
	case "21":
		return "h:mm:ss"
	case "22":
		return "m/d/yy h:mm"
	case "37", "38":
		return "#,##0"
	case "39", "40":
		return "#,##0.00"
	case "44":
		return "$#,##0.00"
	default:
		return ""
	}
}
