package style

// All cmd styling related code should be placed in this file.

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

var (
	TableStyle    = table.StyleLight
	PositiveColor = text.Colors{text.FgGreen}
	NegativeColor = text.Colors{text.FgRed}
	WarningColor  = text.Colors{text.FgYellow}
)

// BoolStr returns a string representation of a boolean value.
func BoolStr(b bool, s ...string) string {
	if len(s) == 2 {
		if b {
			return PositiveColor.Sprint(s[0])
		}
		return NegativeColor.Sprint(s[1])
	}
	if b {
		return PositiveColor.Sprint("yes")
	}
	return NegativeColor.Sprint("no")
}
