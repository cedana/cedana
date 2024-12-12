package checkpoint

import (
	"errors"
	// "github.com/checkpoint-restore/go-criu/v6/crit"
)

const (
	PRETTY     = true
	NO_PAYLOAD = true
)

// Inspects a cedana checkpoint image and returns the information as bytes
// Automatically decompresses if the image is compressed
func Inspect(path string) ([]byte, error) {
	// var xData any
	// var err error

	// c := crit.New("", "", path, PRETTY, NO_PAYLOAD)

	// switch imgType {
	// case "ps":
	// 	xData, err = c.ExplorePs()
	// case "fd", "fds":
	// 	xData, err = c.ExploreFds()
	// case "mem", "mems":
	// 	xData, err = c.ExploreMems()
	// case "rss":
	// 	xData, err = c.ExploreRss()
	// case "gpu":
	// 	xData, err = ExploreGPU(c)
	// default:
	// 	err = errors.New("invalid explore type (supported: {ps|fd|mem|rss|sk|gpu})")
	// }
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to explore image: %v", err)
	// }

	// bytes, err := json.MarshalIndent(xData, "", "  ")
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to marshal data: %v", err)
	// }

	// return bytes, nil

	return nil, errors.New("image inspection is not implemented")
}
