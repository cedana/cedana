package utils

import "fmt"

// List utils

func IntToInt32Slice(slice []int) []int32 {
	var ints []int32

	for _, v := range slice {
		ints = append(ints, int32(v))
	}

	return ints
}

func Int32ToIntSlice(slice []int32) []int {
	var ints []int

	for _, v := range slice {
		ints = append(ints, int(v))
	}

	return ints
}

func IntToUint32Slice(slice []int) []uint32 {
	var ints []uint32

	for _, v := range slice {
		ints = append(ints, uint32(v))
	}

	return ints
}

func Int32ToUint32Slice(slice []int32) []uint32 {
	var ints []uint32

	for _, v := range slice {
		ints = append(ints, uint32(v))
	}

	return ints
}

func Uint32ToStringSlice(slice []uint32) []string {
	var strings []string

	for _, v := range slice {
		strings = append(strings, fmt.Sprintf("%d", v))
	}

	return strings
}

func StringToUint32Slice(slice []string) []uint32 {
	var ints []uint32

	for _, v := range slice {
		var i uint32
		fmt.Sscanf(v, "%d", &i)
		ints = append(ints, i)
	}

	return ints
}
