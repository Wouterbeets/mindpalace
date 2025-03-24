package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// CustomTheme overrides the default theme for a modern look and feel
type CustomTheme struct {
	fyne.Theme
}

//// Color overrides the default theme color for certain elements
//func (t *CustomTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
//	switch name {
//	case theme.ColorNameBackground:
//		return color.NRGBA{R: 30, G: 30, B: 38, A: 255} // Dark navy background
//	case theme.ColorNameForeground:
//		return color.NRGBA{R: 240, G: 240, B: 250, A: 255} // Light text
//	case theme.ColorNamePrimary:
//		return color.NRGBA{R: 65, G: 105, B: 225, A: 255} // Royal blue for primary elements
//	case theme.ColorNameButton:
//		return color.NRGBA{R: 70, G: 130, B: 180, A: 255} // Steel blue for buttons
//	case theme.ColorNameShadow:
//		return color.NRGBA{R: 10, G: 10, B: 15, A: 100} // Subtle shadow
//	case theme.ColorNameInputBackground:
//		return color.NRGBA{R: 40, G: 40, B: 50, A: 255} // Slightly lighter than background
//	default:
//		return t.Theme.Color(name, variant)
//	}
//}
//
//// Size overrides the default sizes for better spacing and readability
//func (t *CustomTheme) Size(name fyne.ThemeSizeName) float32 {
//	switch name {
//	case theme.SizeNameText:
//		return 14 // Slightly larger text
//	case theme.SizeNamePadding:
//		return 8 // More padding for better spacing
//	case theme.SizeNameInlineIcon:
//		return 20 // Larger icons
//	case theme.SizeNameScrollBar:
//		return 10 // Wider scrollbars
//	case theme.SizeNameSeparatorThickness:
//		return 1.5 // Thicker separators
//	default:
//		return t.Theme.Size(name)
//	}
//}

// NewCustomTheme creates a new theme based on the default theme
func NewCustomTheme() *CustomTheme {
	return &CustomTheme{Theme: theme.DefaultTheme()}
}
