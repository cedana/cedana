package validation

// Defines helpers for CRIU compatibility validation

import (
	"context"
	"fmt"

	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
)

const CRIU_MIN_VERSION = 30000

// Check if the installed CRIU version is compatible with the request
func CheckCRIUCompatibility(ctx context.Context, criu *criu.Criu, opts *criu_proto.CriuOpts) error {
	version, err := criu.GetCriuVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get CRIU version: %v", err)
	}

	if version < CRIU_MIN_VERSION {
		return fmt.Errorf("CRIU version %d is not supported. Minimum supported is %d", version, CRIU_MIN_VERSION)
	}

	if opts.LsmProfile != nil {
		if version < 31600 {
			return fmt.Errorf("CRIU version %d does not support LSM profile", version)
		}
	}

	if opts.LsmMountContext != nil {
		if version < 31600 {
			return fmt.Errorf("CRIU version %d does not support LSM mount context", version)
		}
	}

	return nil
}

// Certain CRIU options are not compatible with GPU support.
func CheckCRIUOptsCompatibilityGPU(opts *criu_proto.CriuOpts) error {
	if opts.GetLeaveRunning() {
		return fmt.Errorf("Leave running is not compatible with GPU support, yet")
	}
	return nil
}
