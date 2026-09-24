package csvx

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type xlsxStyles struct {
	NumFmts struct { Items []struct { ID string `xml:"numFmtId,attr"`; Code string `xml:"formatCode,attr"` } `xml:"numFmt"` } `xml:"numFmts"`
	Fonts struct { Items []xlsxFont `xml:"font"` } `xml:"fonts"`
	Fills struct { Items []xlsxFill `xml:"fill"` } `xml:"fills"`
	CellXfs struct { Items []xlsxXF `xml:"xf"` } `xml:"cellXfs"`
}
type xlsxXF struct { NumFmtID string `xml:"numFmtId,attr"`; FontID string `xml:"fontId,attr"`; FillID string `xml:"fillId,attr"`; ApplyNumberFormat string `xml:"applyNumberFormat,attr"` }
type xlsxFont struct { Name []struct { Value string `xml:"val,attr"` } `xml:"name"`; Size []struct { Value string `xml:"val,attr"` } `xml:"sz"`; Bold []struct{} `xml:"b"`; Italic []struct{} `xml:"i"`; Color []xlsxColor `xml:"color"` }
type xlsxFill struct { Pattern []struct { Type string `xml:"patternType,attr"`; Foreground []xlsxColor `xml:"fgColor"` } `xml:"patternFill"` }
type xlsxColor struct { RGB string `xml:"rgb,attr"`; Theme string `xml:"theme,attr"`; Indexed string `xml:"indexed,attr"` }

func parseXLSXStyles(body []byte) (map[string]map[string]any, error) {
	styles := map[string]map[string]any{}
	if len(body) == 0 { return styles, nil }
	var resource xlsxStyles
	if err := xml.Unmarshal(body, &resource); err != nil { return nil, fmt.Errorf("decode XLSX styles: %w", err) }
	formats := map[string]string{}
	for _, item := range resource.NumFmts.Items { formats[item.ID] = item.Code }
	for index, xf := range resource.CellXfs.Items {
		style := map[string]any{}
		if value, ok := formats[xf.NumFmtID]; ok { style["numberFormat"] = value } else if code := builtinNumberFormat(xf.NumFmtID); code != "" { style["numberFormat"] = code }
		if font, ok := indexedFont(resource.Fonts.Items, xf.FontID); ok { style["font"] = font }
		if fill, ok := indexedFill(resource.Fills.Items, xf.FillID); ok { style["fill"] = fill }
		styles[strconv.Itoa(index)] = style
	}
	return styles, nil
}

func indexedFont(fonts []xlsxFont, id string) (map[string]any, bool) { index, err := strconv.Atoi(id); if err != nil || index < 0 || index >= len(fonts) { return nil, false }; source := fonts[index]; font := map[string]any{}; if len(source.Name) > 0 { font["name"] = source.Name[0].Value }; if len(source.Size) > 0 { font["size"] = source.Size[0].Value }; if len(source.Bold) > 0 { font["bold"] = true }; if len(source.Italic) > 0 { font["italic"] = true }; if color := firstColor(source.Color); color != "" { font["color"] = color }; return font, true }
func indexedFill(fills []xlsxFill, id string) (map[string]any, bool) { index, err := strconv.Atoi(id); if err != nil || index < 0 || index >= len(fills) || len(fills[index].Pattern) == 0 { return nil, false }; pattern := fills[index].Pattern[0]; fill := map[string]any{"pattern": pattern.Type}; if color := firstColor(pattern.Foreground); color != "" { fill["color"] = color }; return fill, true }
func firstColor(colors []xlsxColor) string { if len(colors) == 0 { return "" }; color := colors[0]; if color.RGB != "" { rgb := strings.TrimPrefix(strings.ToUpper(color.RGB), "FF"); if len(rgb) == 6 { return "#" + rgb } }; if color.Theme != "" { return themeColor(color.Theme) }; if color.Indexed != "" { return indexedColor(color.Indexed) }; return "" }
func themeColor(theme string) string { colors := map[string]string{"0":"#FFFFFF", "1":"#000000", "2":"#E7E6E6", "3":"#44546A", "4":"#4472C4", "5":"#ED7D31", "6":"#A5A5A5", "7":"#FFC000", "8":"#5B9BD5", "9":"#70AD47"}; return colors[theme] }
func indexedColor(index string) string { colors := map[string]string{"0":"#000000", "1":"#FFFFFF", "2":"#FF0000", "3":"#00FF00", "4":"#0000FF", "5":"#FFFF00", "6":"#FF00FF", "7":"#00FFFF", "8":"#000000", "9":"#FFFFFF", "10":"#FF0000", "11":"#00FF00", "12":"#0000FF", "13":"#FFFF00", "14":"#FF00FF", "15":"#00FFFF"}; return colors[index] }
func builtinNumberFormat(id string) string { switch id { case "0": return "0"; case "1": return "0"; case "2": return "0.00"; case "9": return "0%"; case "10": return "0.00%"; case "14": return "m/d/yy"; default: return "" } }
