package utils

import (
	"fmt"
	"net"
	"reflect"
	"strings"

	"github.com/cedana/cedana/pkg/style"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func GRPCError(err error, extra ...string) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	var extraStr string
	if len(extra) > 0 {
		extraStr = strings.TrimSpace(extra[0])
	}

	if ok {
		if extraStr != "" {
			return fmt.Errorf(
				"%s: %s: %s",
				st.Code().String(),
				st.Message(),
				strings.TrimSpace(extra[0]),
			)
		} else {
			return fmt.Errorf(
				"%s: %s",
				st.Code().String(),
				st.Message(),
			)
		}
	}
	return err
}

func GRPCErrorShort(err error, extra ...string) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	var extraStr string
	if len(extra) > 0 {
		extraStr = strings.TrimSpace(extra[0])
	}
	if ok {
		if extraStr != "" {
			return fmt.Errorf(
				"%s: %s",
				st.Code().String(),
				strings.TrimSpace(extra[0]),
			)
		} else {
			return fmt.Errorf("%s", st.Code().String())
		}
	}
	return err
}

func GRPCErrorColored(err error, extra ...string) error {
	if err == nil {
		return nil
	}
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

func GRPCErrorColoredShort(err error, extra ...string) error {
	if err == nil {
		return nil
	}
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

func VSOCKDialer(contextId uint32, port uint32) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (net.Conn, error) {
		return vsock.Dial(contextId, port, nil)
	}
}
