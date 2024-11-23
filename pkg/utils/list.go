package utils

// List utils

func Int32Slice(slice []int) []int32 {
	var ints []int32

	for _, v := range slice {
		ints = append(ints, int32(v))
	}

	return ints
}
