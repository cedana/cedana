package style

// All cmd styling related code should be placed in this file.

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

var (
	TickMark   = "✔"
	BulletMark = "•"
	CrossMark  = "✖"
	DashMark   = "—"
)

var (
	TableStyle = table.Style{
		Box: table.BoxStyle{
			PaddingRight: "  ",
		},
		Format: table.FormatOptions{
			Header: text.FormatUpper,
		},
		Color: table.ColorOptions{
			Header: text.Colors{text.Bold},
		},
	}
	PositiveColor = text.Colors{text.FgGreen}
	NegativeColor = text.Colors{text.FgRed}
	WarningColor  = text.Colors{text.FgYellow}
	InfoColor     = text.Colors{text.FgHiBlue}
	DisabledColor  = text.Colors{text.FgHiBlack}
)

// BoolStr returns a string representation of a boolean value.
func BoolStr(b bool, s ...string) string {
	if len(s) == 2 {
		if b {
			return PositiveColor.Sprint(s[0])
		}
		return DisabledColor.Sprint(s[1])
	}
	if b {
		return PositiveColor.Sprint("yes")
	}
	return DisabledColor.Sprint("no")
}

// Breaks a like if it's larger than certain length, by adding
// a new line in between. Always breaks at a word boundary.
func BreakLine(s string, length int) string {
	if len(s) > length {
		for i := length; i > 0; i-- {
			if s[i] == ' ' {
				return s[:i] + "\n" + BreakLine(s[i+1:], length)
			}
		}
	}
	return s
}
