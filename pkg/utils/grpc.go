package utils

import (
	"fmt"
	"reflect"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

func LogProtoMessage(msg proto.Message, subject string, level zerolog.Level) {
	val := reflect.ValueOf(msg)
	v := reflect.Indirect(val)
	for i := 0; i < v.NumField(); i++ {
		st := v.Type()
		name := st.Field(i).Name
		if 'A' <= name[0] && name[0] <= 'Z' {
			value := val.MethodByName("Get" + name).Call([]reflect.Value{})
			log.WithLevel(level).Interface(name, value[0]).Msg(subject)
		}
	}
}
