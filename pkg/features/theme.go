package features

import "github.com/jedib0t/go-pretty/v6/text"

func Theme() map[string]text.Colors {
	colorMap := make(map[string]text.Colors)
	CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
		colorMap[name] = theme
		return nil
	})
	return colorMap
}
