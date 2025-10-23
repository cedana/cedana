package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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
	var totalDuration time.Duration
	var totalIO int64

	fmt.Print("Profiling data received:\n\n")

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)

	categoryDuration := make(map[string]time.Duration)
	categoryIO := make(map[string]int64)
	precision := config.Global.Profiling.Precision

	for _, p := range data.Components {
		if p.Duration == 0 && p.IO == 0 {
			continue
		}
		categoryName, name := utils.SimplifyFuncName(p.Name)

		category := style.WarningColors.Sprint(categoryName)
		features.CmdTheme.IfAvailable(func(name string, theme text.Colors) error {
			category = theme.Sprint(categoryName)
			return nil
		}, categoryName)

		duration := time.Duration(p.Duration)
		durationStr := profiling.DurationStr(duration, precision)
		if p.Parallel {
			durationStr = style.DisabledColors.Sprint("├──" + durationStr)
		} else {
			totalDuration += duration // Don't count parallel durations towards total
		}
		totalIO += p.IO

		if categoryName != "" {
			categoryDuration[category] += duration
			categoryIO[category] += p.IO
		} else {
			categoryDuration[style.DisabledColors.Sprint("other")] += duration
			categoryIO[style.DisabledColors.Sprint("other")] += p.IO
		}

		tableWriter.AppendRow([]any{
			durationStr,
			category,
			utils.SizeStr(p.IO),
			style.DisabledColors.Sprint(name),
		})
	}

	tableWriter.AppendFooter([]any{
		profiling.DurationStr(totalDuration, precision),
		"",
		utils.SizeStr(totalIO),
		fmt.Sprintf("%s (total)", data.Name),
	})
	tableWriter.Render()

	if len(categoryDuration) > 1 {
		fmt.Println()
		tableWriter = table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		for category, duration := range categoryDuration {
			percentage := (float64(duration) / float64(totalDuration)) * 100
			tableWriter.AppendRow([]any{
				profiling.DurationStr(duration, precision),
				fmt.Sprintf("%.2f%%", percentage),
				utils.SizeStr(categoryIO[category]),
				category,
			})
		}

		tableWriter.AppendFooter([]any{
			profiling.DurationStr(totalDuration, precision),
			"",
			utils.SizeStr(totalIO),
			fmt.Sprintf("%s (total)", data.Name),
		})
		tableWriter.Render()
	}

	fmt.Println()
}

func printHealthCheckResults(results []*daemon.HealthCheckResult) error {
	errorCount := 0
	warningCount := 0

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)

	for _, result := range results {
		tableWriter.AppendSeparator()
		name := strings.ToUpper(result.Name)
		tableWriter.AppendRow(table.Row{text.Bold.Sprint(name), "", ""})
		for _, component := range result.Components {
			statusErr := style.NegativeColors.Sprint(style.CrossMark)
			statusWarn := style.WarningColors.Sprint(style.BulletMark)
			statusOk := style.PositiveColors.Sprint(style.TickMark)
			data := component.Data
			var status string
			if len(component.Errors) > 0 {
				status = statusErr
				data = style.NegativeColors.Sprint(data)
			} else if len(component.Warnings) > 0 {
				status = statusWarn
				data = style.WarningColors.Sprint(data)
			} else {
				status = statusOk
			}

			maxLinelen := 60
			rows := []table.Row{{component.Name, data, status}}
			for _, err := range component.Errors {
				errorCount++
				err = style.BreakLine(err, maxLinelen)
				err = style.DisabledColors.Sprint(err)
				if len(rows) == 1 && len(rows[0]) == 3 {
					rows[0] = append(rows[0], err)
					continue
				}
				rows = append(rows, table.Row{"", "", statusErr, err})
			}
			for _, warn := range component.Warnings {
				warningCount++
				warn = style.BreakLine(warn, maxLinelen)
				warn = style.DisabledColors.Sprint(warn)
				if len(rows) == 1 && len(rows[0]) == 3 {
					rows[0] = append(rows[0], warn)
					continue
				}
				rows = append(rows, table.Row{"", "", statusWarn, warn})
			}

			tableWriter.AppendRows(rows)
		}
	}

	tableWriter.Render()
	fmt.Println()

	if errorCount > 0 {
		if warningCount > 0 {
			return fmt.Errorf("Failed with %d error(s) and %d warning(s).", errorCount, warningCount)
		}
		return fmt.Errorf("Failed with %d error(s).", errorCount)
	} else if warningCount > 0 {
		fmt.Printf("Looks good, with %d warning(s).\n", warningCount)
	} else {
		fmt.Println("All good.")
	}

	return nil
}
