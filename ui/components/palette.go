package components

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/color"
)

type Palette struct {
	Primary    color.Color `json:"primary" toml:"primary"`
	Secondary  color.Color `json:"secondary" toml:"secondary"`
	Accent     color.Color `json:"accent" toml:"accent"`
	Background color.Color `json:"background" toml:"background"`
}

func (p Palette) MarshalJSON() ([]byte, error) {
	colors := map[string]color.Color{
		"primary": p.Primary, "secondary": p.Secondary,
		"accent": p.Accent, "background": p.Background,
	}
	values := make(map[string]string, len(colors))
	for name, c := range colors {
		if c == nil {
			return nil, fmt.Errorf("palette %s: nil color", name)
		}
		rgba := color.NRGBAModel.Convert(c).(color.NRGBA)
		value := fmt.Sprintf("#%02X%02X%02X", rgba.R, rgba.G, rgba.B)
		if rgba.A != 255 {
			value += fmt.Sprintf("%02X", rgba.A)
		}
		values[name] = value
	}
	return json.Marshal(values)
}

func (p *Palette) UnmarshalJSON(data []byte) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if values == nil {
		return fmt.Errorf("palette must be a JSON object")
	}
	next := *p
	fields := map[string]*color.Color{
		"primary": &next.Primary, "secondary": &next.Secondary,
		"accent": &next.Accent, "background": &next.Background,
	}
	for name, field := range fields {
		raw, ok := values[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("palette %s: %w", name, err)
		}
		if (len(value) != 7 && len(value) != 9) || value[0] != '#' {
			return fmt.Errorf("palette %s: expected #RRGGBB or #RRGGBBAA, got %q", name, value)
		}
		bytes, err := hex.DecodeString(value[1:])
		if err != nil {
			return fmt.Errorf("palette %s: invalid hex color %q: %w", name, value, err)
		}
		c := color.NRGBA{R: bytes[0], G: bytes[1], B: bytes[2], A: 255}
		if len(bytes) == 4 {
			c.A = bytes[3]
		}
		*field = c
	}
	*p = next
	return nil
}
