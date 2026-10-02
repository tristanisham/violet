package components

import (
	"encoding/json"
	"image/color"
	"strings"
	"testing"
)

func TestPaletteJSONRoundtrip(t *testing.T) {
	original := Palette{
		Primary:    color.NRGBA{R: 0x7F, B: 0xFF, A: 255},
		Secondary:  color.NRGBA{R: 0xB1, G: 0x63, B: 0xFF, A: 255},
		Accent:     color.NRGBA{R: 0x51, G: 0x15, B: 0x8C, A: 128},
		Background: color.NRGBA{R: 0x2B, B: 0x57, A: 255},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"primary":"#7F00FF"`, `"secondary":"#B163FF"`, `"accent":"#51158C80"`, `"background":"#2B0057"`} {
		if !strings.Contains(string(data), value) {
			t.Errorf("JSON %s does not contain %s", data, value)
		}
	}
	var decoded Palette
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("decoded = %+v, want %+v", decoded, original)
	}
	if err := json.Unmarshal([]byte(`{"primary":"#abcdef"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Primary != (color.NRGBA{R: 0xAB, G: 0xCD, B: 0xEF, A: 255}) || decoded.Secondary != original.Secondary || decoded.Accent != original.Accent || decoded.Background != original.Background {
		t.Fatalf("partial overlay = %+v", decoded)
	}
}

func TestPaletteInvalidJSON(t *testing.T) {
	for _, data := range []string{
		`{"primary":"#GG0000"}`, `{"primary":"123456"}`,
		`{"primary":"#123"}`, `{"primary":"#1234567"}`,
		`{"primary":""}`, `{"primary":null}`, `{"primary":123}`, `null`, `[]`, `{`,
	} {
		var palette Palette
		if err := json.Unmarshal([]byte(data), &palette); err == nil {
			t.Fatalf("accepted invalid palette %s", data)
		}
	}
	if _, err := json.Marshal(Palette{}); err == nil {
		t.Fatal("accepted nil palette colors")
	}
}
