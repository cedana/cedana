package utils

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/afero"
)

func SaveScriptToDump(script *os.File, fn string, fs ...afero.Fs) error {
	var filesystem afero.Fs

	if len(fs) > 0 {
		filesystem = fs[0]
	} else {
		filesystem = afero.NewOsFs()
	}

	contents, err := io.ReadAll(script)
	if err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	file, err := filesystem.Create(fn)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	_, err = file.Write(contents)
	if err != nil {
		return fmt.Errorf("error writing script data: %v", err)
	}

	return nil
}

func LoadScriptFromDump(fn string, fs ...afero.Fs) ([]byte, error) {
	var err error
	var file afero.File

	// Open the file to read the script contents
	if len(fs) > 0 {
		file, err = fs[0].Open(fn)
	} else {
		file, err = os.Open(fn)
	}
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Read all contents
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("error reading script contents: %v", err)
	}

	return contents, nil
}
