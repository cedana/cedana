package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/style"
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

func PrintHealthCheckResults(results []*daemon.HealthCheckResult) error {
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
