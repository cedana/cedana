package utils

import "context"

func GetContextValSafe[T any](ctx context.Context, key any, default_val T) (val T) {
	val, ok := ctx.Value(key).(T)
	if !ok {
		val = default_val
	}
	return
}
