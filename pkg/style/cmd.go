package style

// All cmd styling related code should be placed in this file.

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
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
	DisbledColor  = text.Colors{text.FgHiBlack}
)

// BoolStr returns a string representation of a boolean value.
func BoolStr(b bool, s ...string) string {
	if len(s) == 2 {
		if b {
			return PositiveColor.Sprint(s[0])
		}
		return DisbledColor.Sprint(s[1])
	}
	if b {
		return PositiveColor.Sprint("yes")
	}
	return DisbledColor.Sprint("no")
}
