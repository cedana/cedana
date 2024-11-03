package utils

// Common function utilities

func IgnoreErr[T any](f func() (T, error)) T {
	res, _ := f()
	return res
}
