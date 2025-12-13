package style

// All cmd styling related code should be placed in this file.

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const MAX_LINE_LENGTH = 80

var (
	TickMark   = "✔"
	BulletMark = "•"
	CrossMark  = "✖"
	DashMark   = "—"
)

var (
	TableStyle = table.Style{
		Options: table.Options{
			SeparateHeader:  false,
			SeparateRows:    false,
			SeparateColumns: false,
			SeparateFooter:  false,
		},
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
	PositiveColors = text.Colors{text.FgGreen}
	NegativeColors = text.Colors{text.FgRed}
	WarningColors  = text.Colors{text.FgYellow}
	InfoColors     = text.Colors{text.FgHiBlue}
	DisabledColors = text.Colors{text.FgHiBlack}

	HighLevelRuntimeColors = text.Colors{text.FgMagenta}
	LowLevelRuntimeColors  = text.Colors{text.FgCyan}
)

// BoolStr returns a string representation of a boolean value.
func BoolStr(b bool, s ...string) string {
	if len(s) == 2 {
		if b {
			return PositiveColors.Sprint(s[0])
		}
		return DisabledColors.Sprint(s[1])
	}
	if b {
		return PositiveColors.Sprint("yes")
	}
	return DisabledColors.Sprint("no")
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
