package utils

import (
	"encoding/json"
	"fmt"
	"os"
)

func SaveJSONToFile(data any, path string) error {
	// Marshal the struct to JSON
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	// Create or open a file to write the JSON data
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	// Write JSON data to the file
	_, err = file.Write(jsonData)
	if err != nil {
		return fmt.Errorf("error writing JSON data: %v", err)
	}

	return nil
}

func LoadJSONFromFile(path string, data any) error {
	// Open the file to read the JSON data
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	// Decode the JSON data into the struct
	decoder := json.NewDecoder(file)
	err = decoder.Decode(data)
	if err != nil {
		return fmt.Errorf("error decoding JSON: %v", err)
	}

	return nil
}
