package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

func getRevision() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return setting.Value
			}
		}
	}
	return ""
}

// PrintProfilingData prints the profiling data in a very readable format.
func printProfilingData(data *profiling.Data) {
	var total time.Duration

	fmt.Print("Profiling data received:\n\n")

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)

	categoryMap := make(map[string]time.Duration)
	precision := config.Global.Profiling.Precision

	for _, p := range data.Components {
		if p.Duration == 0 {
			continue
		}
		categoryName, name := utils.SimplifyFuncName(p.Name)

		category := style.WarningColors.Sprint(categoryName)
		features.CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
			category = theme.Sprint(categoryName)
			return nil
		}, categoryName)

		duration := time.Duration(p.Duration)
		total += duration

		if categoryName != "" {
			categoryMap[category] += duration
		} else {
			categoryMap[style.DisabledColors.Sprint("other")] += duration
		}

		tableWriter.AppendRow([]interface{}{profiling.DurationStr(duration, precision), category, style.DisabledColors.Sprint(name)})
	}

	tableWriter.AppendFooter([]interface{}{profiling.DurationStr(total, precision), "", fmt.Sprintf("%s (total)", data.Name)})
	tableWriter.Render()

	if len(categoryMap) > 1 {
		fmt.Println()
		tableWriter = table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		for category, duration := range categoryMap {
			percentage := (float64(duration) / float64(total)) * 100
			tableWriter.AppendRow([]interface{}{profiling.DurationStr(duration, precision), fmt.Sprintf("%.2f%%", percentage), category})
		}

		tableWriter.AppendFooter([]interface{}{profiling.DurationStr(total, precision), "", fmt.Sprintf("%s (total)", data.Name)})
		tableWriter.Render()
	}

	fmt.Println()
}
