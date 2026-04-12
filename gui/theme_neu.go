package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type neuTheme struct {
	base fyne.Theme
}

func newNEUTheme() fyne.Theme {
	return neuTheme{base: theme.DefaultTheme()}
}

func (t neuTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	// Northeastern (NEU) inspired palette: red + black accents.
	neuRed := color.NRGBA{R: 0xC8, G: 0x10, B: 0x2E, A: 0xFF}
	neuRedDark := color.NRGBA{R: 0xA3, G: 0x0D, B: 0x25, A: 0xFF}
	neuBlack := color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xFF}
	neuNearWhite := color.NRGBA{R: 0xFA, G: 0xFA, B: 0xFB, A: 0xFF}
	neuDarkBg := color.NRGBA{R: 0x0D, G: 0x0D, B: 0x0F, A: 0xFF}

	switch name {
	case theme.ColorNamePrimary:
		if variant == theme.VariantDark {
			return neuRed
		}
		return neuRed
	case theme.ColorNameButton:
		if variant == theme.VariantDark {
			return neuRedDark
		}
		return neuRed
	case theme.ColorNameFocus, theme.ColorNameSelection, theme.ColorNameHover:
		if variant == theme.VariantDark {
			return neuRedDark
		}
		return neuRed
	case theme.ColorNameForeground:
		if variant == theme.VariantDark {
			return neuNearWhite
		}
		return neuBlack
	case theme.ColorNameBackground:
		if variant == theme.VariantDark {
			return neuDarkBg
		}
		return neuNearWhite
	}

	return t.base.Color(name, variant)
}

func (t neuTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t neuTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t neuTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

