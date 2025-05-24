package io

import "os"

type (
	WriteToFunc  func(src *os.File, target string, compression string) (int64, error)
	ReadFromFunc func(src string, target *os.File) (int64, error)

	Storage struct {
		Remote   bool
		WriteTo  WriteToFunc
		ReadFrom ReadFromFunc
	}
)
