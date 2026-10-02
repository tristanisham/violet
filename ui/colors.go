package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type Palette struct {
	Primary    color.Color
	Secondary  color.Color
	Accent     color.Color
	Background color.Color
}

var VioletPalette = Palette{
	Primary:    lipgloss.Color("#7F00FF"), // Violet
	Secondary:  lipgloss.Color("#B163FF"), // Light Violet
	Accent:     lipgloss.Color("#51158C"), // Deep purple
	Background: lipgloss.Color("#2B0057"), // Dark purple
}

// MistyPalette is based on https://www.figma.com/colors/misty-blue/.
var MistyPalette = Palette{
	Primary:    lipgloss.Color("#B5C7EB"),
	Secondary:  lipgloss.Color("#94AEE3"),
	Accent:     lipgloss.Color("#94A3C0"),
	Background: lipgloss.Color("#60697C"),
}

// ForestPalette is based on https://www.figma.com/colors/forest-green/.
var ForestPalette = Palette{
	Primary:    lipgloss.Color("#2E6F40"),
	Secondary:  lipgloss.Color("#CFFFDC"),
	Accent:     lipgloss.Color("#68BA7F"),
	Background: lipgloss.Color("#253D2C"),
}

// SlatePalette is based on https://www.figma.com/colors/slate-gray/.
var SlatePalette = Palette{
	Primary:    lipgloss.Color("#6D8196"),
	Secondary:  lipgloss.Color("#6D8196"),
	Accent:     lipgloss.Color("#546373"),
	Background: lipgloss.Color("#36404A"),
}

var DefaultPalette = VioletPalette
