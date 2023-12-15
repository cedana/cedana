package api

import (
	"fmt"
	"os"
)

func RuncList(root string) error {
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	fileInfos, err := dir.Readdir(-1)
	if err != nil {
		return err
	}

	var runcContainers []string // Slice to hold directory names

	for _, fileInfo := range fileInfos {
		if fileInfo.IsDir() {
			runcContainers = append(runcContainers, fileInfo.Name())
			fmt.Println(fileInfo.Name())
		}
	}
	return nil
}
