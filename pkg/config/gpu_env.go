package config

import "github.com/cedana/cedana/pkg/utils"

// GPUClientEnv returns the env vars to add to an app started with GPU interception: the
// settings the interception library reads inside the app process (the GPU controller gets its
// own env when spawned). env is the app's env; a variable it already sets is left out, so a
// workload's own value takes precedence over the config.
func GPUClientEnv(env []string) []string {
	vars := []struct{ key, value string }{
		// Read as 0/1: the library treats any value other than "0" as on
		{"CEDANA_GPU_IMAGE_ZERO_COPY", boolEnv(Global.GPU.ImageZeroCopy)},
	}
	var add []string
	for _, v := range vars {
		if utils.Getenv(env, v.key) == "" {
			add = append(add, v.key+"="+v.value)
		}
	}
	return add
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
