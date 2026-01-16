package profiling

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// Data is a struct that represents a profiling data tree.
type Data struct {
	Name       string  `json:"name"`
	Components []*Data `json:"components,omitempty"`

	Duration int64 `json:"duration,omitempty"`
	IO       int64 `json:"io,omitempty"`
	// add more othogonal fields here as needed

	Parallel  bool `json:"parallel,omitempty"`
	Redundant bool `json:"redundant,omitempty"`
}

// Flatten flattens the profiling data into a single list of components.
// This is such that the duration of each component is purely the time spent in that component
// excluding the time spent in its children.
func Flatten(data *Data) {
	length := len(data.Components)

	for i := range length {
		component := data.Components[i]

		if !(component.Parallel || component.Redundant) { // only consider duration of serial components in calculating remainder
			data.Duration -= component.Duration
		}

		Flatten(component)

		data.Components = append(data.Components, component.Components...)
		component.Components = nil
	}

	// If the data has exactly 0 duration, then it was just a category wrapper for its components.
	// so we append its name to the name of its children.

	if data.Duration == 0 && data.Name != "" {
		for _, component := range data.Components {
			component.Name = data.Name + ":" + component.Name
		}
	}
}

// Clean collapses any empty wrappers in the profiling data.
// Empty wrappers are those that have no duration, no IO, no name, and only components.
func Clean(data *Data) {
	length := len(data.Components)
	if length == 0 {
		return
	}

	newComponents := make([]*Data, 0, length)

	for i := range length {
		component := data.Components[i]

		Clean(component)

		if component.Duration == 0 && component.IO == 0 && component.Name == "" {
			newComponents = append(newComponents, component.Components...)
		} else {
			newComponents = append(newComponents, component)
		}
	}

	data.Components = newComponents
}

// Print prints the profiling data in a very readable format.
func Print(data *Data, categoryColors ...map[string]text.Colors) {
	var totalDuration time.Duration
	var totalIO int64

	tableWriter := table.NewWriter()
	tableWriter.SetStyle(style.TableStyle)
	tableWriter.SetOutputMirror(os.Stdout)

	categoryDuration := make(map[string]time.Duration)
	categoryIO := make(map[string]int64)
	categoryIORedundant := make(map[string]bool)
	precision := config.Global.Profiling.Precision

	for _, p := range data.Components {
		if p.Duration == 0 && p.IO == 0 {
			continue
		}
		categoryName, name := utils.SimplifyFuncName(p.Name)

		category := style.WarningColors.Sprint(categoryName)
		if len(categoryColors) > 0 {
			if theme, ok := categoryColors[0][categoryName]; ok {
				category = theme.Sprint(categoryName)
			}
		}

		duration := time.Duration(p.Duration)
		durationStr := DurationStr(duration, precision)
		io := p.IO
		ioStr := utils.SizeStr(io)
		if p.Parallel || p.Redundant {
			durationStr = style.DisabledColors.Sprint(durationStr + "──┤")
			duration = 0 // Don't count parallel durations towards total
			if p.Redundant {
				ioStr = style.DisabledColors.Sprint(ioStr)
				io = 0 // Don't count redundant IO towards total
			}
		}
		totalDuration += duration
		totalIO += io

		if categoryName != "" {
			categoryDuration[category] += duration
			categoryIO[category] += p.IO
			categoryIORedundant[category] = p.Redundant
		} else {
			categoryDuration[style.DisabledColors.Sprint("other")] += duration
			categoryIO[style.DisabledColors.Sprint("other")] += p.IO
			categoryIORedundant[style.DisabledColors.Sprint("other")] = p.Redundant
		}

		tableWriter.AppendRow([]any{
			durationStr,
			category,
			ioStr,
			style.DisabledColors.Sprint(name),
		})
	}

	tableWriter.AppendFooter([]any{
		DurationStr(totalDuration, precision),
		"",
		utils.SizeStr(totalIO),
		fmt.Sprintf("%s (total)", data.Name),
	})
	tableWriter.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
		{Number: 2, Align: text.AlignLeft, AlignHeader: text.AlignLeft, AlignFooter: text.AlignLeft},
		{Number: 3, Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
		{Number: 4, Align: text.AlignLeft, AlignHeader: text.AlignLeft, AlignFooter: text.AlignLeft},
	})

	if config.Global.Profiling.Detailed {
		tableWriter.Render()
	}

	if len(categoryDuration) > 1 {
		if config.Global.Profiling.Detailed {
			fmt.Println()
		}
		tableWriter = table.NewWriter()
		tableWriter.SetStyle(style.TableStyle)
		tableWriter.SetOutputMirror(os.Stdout)

		for category, duration := range categoryDuration {
			percentage := (float64(duration) / float64(totalDuration)) * 100
			ioStr := utils.SizeStr(categoryIO[category])
			if categoryIORedundant[category] {
				ioStr = style.DisabledColors.Sprint(ioStr)
			}
			tableWriter.AppendRow([]any{
				DurationStr(duration, precision),
				fmt.Sprintf("%.2f%%", percentage),
				ioStr,
				category,
			})
		}

		tableWriter.AppendFooter([]any{
			DurationStr(totalDuration, precision),
			"",
			utils.SizeStr(totalIO),
			fmt.Sprintf("%s (total)", data.Name),
		})
		tableWriter.SetColumnConfigs([]table.ColumnConfig{
			{Number: 1, Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Number: 2, Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Number: 3, Align: text.AlignRight, AlignHeader: text.AlignRight, AlignFooter: text.AlignRight},
			{Number: 4, Align: text.AlignLeft, AlignHeader: text.AlignLeft, AlignFooter: text.AlignLeft},
		})
		tableWriter.Render()
	}

	fmt.Println()
}

func EncodeJSON(data *Data) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func WriteJSON(path string, data *Data) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(path, bytes, 0o644)
}

func ReadJSON(path string) (*Data, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data Data
	err = json.Unmarshal(bytes, &data)

	return &data, err
}

func Encode(data *Data, buf *bytes.Buffer) error {
	return gob.NewEncoder(buf).Encode(data)
}

func Decode(data string) (*Data, error) {
	var d Data
	err := gob.NewDecoder(bytes.NewBufferString(data)).Decode(&d)
	return &d, err
}
