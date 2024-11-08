package utils

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GRPCError(err error) error {
	st, ok := status.FromError(err)
	if ok {
		if st.Code() == codes.Unavailable {
			return fmt.Errorf("Daemon unavailable. Is it running?")
		} else {
			return fmt.Errorf("%s: %s", st.Code().String(), st.Message())
		}
	}
	return err
}
