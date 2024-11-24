package utils

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/cedana/cedana/pkg/style"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func GRPCError(err error, extra ...string) error {
	st, ok := status.FromError(err)
	var extraStr string
	if len(extra) > 0 {
		extraStr = strings.TrimSpace(extra[0])
	}

	if ok {
		if extraStr != "" {
			return fmt.Errorf(
				"%s: %s: %s",
				style.NegativeColor.Sprint(st.Code().String()),
				st.Message(),
				strings.TrimSpace(extra[0]),
			)
		} else {
			return fmt.Errorf(
				"%s: %s",
				style.NegativeColor.Sprint(st.Code().String()),
				st.Message(),
			)
		}
	}
	return err
}

func GRPCErrorShort(err error, extra ...string) error {
	st, ok := status.FromError(err)
	var extraStr string
	if len(extra) > 0 {
		extraStr = strings.TrimSpace(extra[0])
	}
	if ok {
		if extraStr != "" {
			return fmt.Errorf(
				"%s: %s",
				style.NegativeColor.Sprint(st.Code().String()),
				strings.TrimSpace(extra[0]),
			)
		} else {
			return fmt.Errorf("%s", style.NegativeColor.Sprint(st.Code().String()))
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
