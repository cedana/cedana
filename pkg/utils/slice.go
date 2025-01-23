package utils

// List utils

func IntToInt32Slice(slice []int) []int32 {
	var ints []int32

	for _, v := range slice {
		ints = append(ints, int32(v))
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
