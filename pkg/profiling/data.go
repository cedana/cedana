package profiling

import (
	"bytes"
	"encoding/gob"
)

// Data is a struct that represents a profiling data tree.
type Data struct {
	Name       string
	Components []*Data

	Duration int64
	// add more othogonal fields here as needed
}

// FlattenData flattens the profiling data into a single list of components.
// This is such that the duration of each component is purely the time spent in that component
// excluding the time spent in its children.
func FlattenData(data *Data) {
	length := len(data.Components)

	for i := 0; i < length; i++ {
		component := data.Components[i]

		data.Duration -= component.Duration

		FlattenData(component)

		data.Components = append(data.Components, component.Components...)
		component.Components = nil

	}

	// If the data has exactly 0 duration now, then it was just a category wrapper for its components.
	// so we append its name to the name of its children.

	if data.Duration == 0 && data.Name != "" {
		for _, component := range data.Components {
			component.Name = data.Name + ":" + component.Name
		}
	}
}

// CleanData collapses any empty wrappers in the profiling data.
// Empty wrappers are those that have no duration, no name, and only components.
func CleanData(data *Data) {
	length := len(data.Components)
	if length == 0 {
		return
	}

	newComponents := make([]*Data, 0, length)

	for i := 0; i < length; i++ {
		component := data.Components[i]

		CleanData(component)

		if component.Duration == 0 && component.Name == "" {
			newComponents = append(newComponents, component.Components...)
		} else {
			newComponents = append(newComponents, component)
		}
	}

	data.Components = newComponents
}

func Encode(data *Data, buf *bytes.Buffer) error {
	return gob.NewEncoder(buf).Encode(data)
}

func Decode(data string) (*Data, error) {
	var d Data
	err := gob.NewDecoder(bytes.NewBufferString(data)).Decode(&d)
	return &d, err
}
