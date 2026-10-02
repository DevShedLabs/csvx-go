package csvx

import (
	"encoding/json"

	"github.com/DevShedLabs/csvx-go/internal/schema"
)

// PrintSettings is a sheet's print and pagination settings (spec/03-sheets.md, "Print settings").
// Its fields are generated from csvx-spec's sheet-metadata.schema.json. Properties the schema does
// not define are kept in AdditionalProperties and written back out, as the spec requires.
type PrintSettings schema.Print

// UnmarshalJSON decodes through the generated type so enums and ranges are validated and unknown
// properties land in AdditionalProperties.
func (p *PrintSettings) UnmarshalJSON(data []byte) error {
	var decoded schema.Print
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = PrintSettings(decoded)
	return nil
}

// MarshalJSON writes the typed fields plus any preserved unknown properties, without the generated
// AdditionalProperties bucket key itself.
func (p PrintSettings) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(schema.Print(p))
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(flattenAdditionalProperties(generic))
}

// flattenAdditionalProperties replaces each generated "AdditionalProperties" bucket with its
// entries, recursively. Typed fields win over a same-named unknown property.
func flattenAdditionalProperties(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		extra, _ := typed["AdditionalProperties"].(map[string]any)
		delete(typed, "AdditionalProperties")
		for key, item := range typed {
			typed[key] = flattenAdditionalProperties(item)
		}
		for key, item := range extra {
			if _, exists := typed[key]; !exists {
				typed[key] = item
			}
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = flattenAdditionalProperties(item)
		}
	}
	return value
}
