// Package preferences owns user-local terminal presentation preferences.
// These values affect rendering only and never participate in governance.
package preferences

import (
	"fmt"
	"strings"
)

const SchemaVersion = 1

type Color string

const (
	ColorDefault  Color = "default"
	ColorTerminal Color = "terminal"
	ColorBlack    Color = "black"
	ColorWhite    Color = "white"
	ColorGray     Color = "gray"
	ColorCyan     Color = "cyan"
	ColorBlue     Color = "blue"
	ColorGreen    Color = "green"
	ColorYellow   Color = "yellow"
	ColorRed      Color = "red"
	ColorMagenta  Color = "magenta"
)

type Sidebar struct {
	Visible  bool `json:"visible"`
	Identity bool `json:"identity"`
	Context  bool `json:"context"`
	Provider bool `json:"provider"`
	Status   bool `json:"status"`
}

type Colors struct {
	Accent     Color `json:"accent"`
	Border     Color `json:"border"`
	Background Color `json:"background"`
	Text       Color `json:"text"`
}

type Layout struct {
	SchemaVersion int     `json:"schema_version"`
	Sidebar       Sidebar `json:"sidebar"`
	Colors        Colors  `json:"colors"`
}

func Defaults() Layout {
	return Layout{
		SchemaVersion: SchemaVersion,
		Sidebar: Sidebar{
			Visible: true, Identity: true, Context: true, Provider: true, Status: true,
		},
		Colors: Colors{
			Accent: ColorCyan, Border: ColorBlue, Background: ColorBlack, Text: ColorWhite,
		},
	}
}

func (layout Layout) Validate() error {
	if layout.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported presentation preference schema version %d", layout.SchemaVersion)
	}
	if !validForeground(layout.Colors.Accent) {
		return fmt.Errorf("invalid accent color %q", layout.Colors.Accent)
	}
	if !validForeground(layout.Colors.Border) {
		return fmt.Errorf("invalid border color %q", layout.Colors.Border)
	}
	if layout.Colors.Background != ColorTerminal && !validColor(layout.Colors.Background) {
		return fmt.Errorf("invalid background color %q", layout.Colors.Background)
	}
	if !validForeground(layout.Colors.Text) {
		return fmt.Errorf("invalid text color %q", layout.Colors.Text)
	}
	if layout.Colors.Background != ColorTerminal {
		for _, candidate := range []struct {
			role  string
			color Color
		}{
			{role: "accent", color: layout.Colors.Accent},
			{role: "border", color: layout.Colors.Border},
			{role: "text", color: layout.Colors.Text},
		} {
			if candidate.color != ColorDefault && candidate.color == layout.Colors.Background {
				return fmt.Errorf("%s color %q is not readable on the same background", candidate.role, candidate.color)
			}
		}
	}
	return nil
}

func ParseColor(value string, background bool) (Color, error) {
	color := Color(strings.ToLower(strings.TrimSpace(value)))
	if background && color == ColorDefault {
		color = ColorTerminal
	}
	if (background && (color == ColorTerminal || validColor(color))) || (!background && validForeground(color)) {
		return color, nil
	}
	return "", fmt.Errorf("unknown presentation color %q", value)
}

func ColorNames(background bool) []string {
	names := []string{"default", "black", "white", "gray", "cyan", "blue", "green", "yellow", "red", "magenta"}
	if background {
		names[0] = "terminal"
	}
	return names
}

func validForeground(color Color) bool {
	return color == ColorDefault || validColor(color)
}

func validColor(color Color) bool {
	switch color {
	case ColorBlack, ColorWhite, ColorGray, ColorCyan, ColorBlue, ColorGreen, ColorYellow, ColorRed, ColorMagenta:
		return true
	default:
		return false
	}
}
