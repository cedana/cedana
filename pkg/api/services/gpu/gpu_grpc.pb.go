// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v5.28.0
// source: gpu.proto

package gpu

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// CedanaGPUClient is the client API for CedanaGPU service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type CedanaGPUClient interface {
	Checkpoint(ctx context.Context, in *CheckpointRequest, opts ...grpc.CallOption) (*CheckpointResponse, error)
	Restore(ctx context.Context, in *RestoreRequest, opts ...grpc.CallOption) (*RestoreResponse, error)
	StartupPoll(ctx context.Context, in *StartupPollRequest, opts ...grpc.CallOption) (*StartupPollResponse, error)
	HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
}

type cedanaGPUClient struct {
	cc grpc.ClientConnInterface
}

func NewCedanaGPUClient(cc grpc.ClientConnInterface) CedanaGPUClient {
	return &cedanaGPUClient{cc}
}

func (c *cedanaGPUClient) Checkpoint(ctx context.Context, in *CheckpointRequest, opts ...grpc.CallOption) (*CheckpointResponse, error) {
	out := new(CheckpointResponse)
	err := c.cc.Invoke(ctx, "/cedanagpu.CedanaGPU/Checkpoint", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cedanaGPUClient) Restore(ctx context.Context, in *RestoreRequest, opts ...grpc.CallOption) (*RestoreResponse, error) {
	out := new(RestoreResponse)
	err := c.cc.Invoke(ctx, "/cedanagpu.CedanaGPU/Restore", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cedanaGPUClient) StartupPoll(ctx context.Context, in *StartupPollRequest, opts ...grpc.CallOption) (*StartupPollResponse, error) {
	out := new(StartupPollResponse)
	err := c.cc.Invoke(ctx, "/cedanagpu.CedanaGPU/StartupPoll", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cedanaGPUClient) HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	out := new(HealthCheckResponse)
	err := c.cc.Invoke(ctx, "/cedanagpu.CedanaGPU/HealthCheck", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CedanaGPUServer is the server API for CedanaGPU service.
// All implementations must embed UnimplementedCedanaGPUServer
// for forward compatibility
type CedanaGPUServer interface {
	Checkpoint(context.Context, *CheckpointRequest) (*CheckpointResponse, error)
	Restore(context.Context, *RestoreRequest) (*RestoreResponse, error)
	StartupPoll(context.Context, *StartupPollRequest) (*StartupPollResponse, error)
	HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
	mustEmbedUnimplementedCedanaGPUServer()
}

// UnimplementedCedanaGPUServer must be embedded to have forward compatible implementations.
type UnimplementedCedanaGPUServer struct {
}

func (UnimplementedCedanaGPUServer) Checkpoint(context.Context, *CheckpointRequest) (*CheckpointResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Checkpoint not implemented")
}
func (UnimplementedCedanaGPUServer) Restore(context.Context, *RestoreRequest) (*RestoreResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Restore not implemented")
}
func (UnimplementedCedanaGPUServer) StartupPoll(context.Context, *StartupPollRequest) (*StartupPollResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartupPoll not implemented")
}
func (UnimplementedCedanaGPUServer) HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method HealthCheck not implemented")
}
func (UnimplementedCedanaGPUServer) mustEmbedUnimplementedCedanaGPUServer() {}

// UnsafeCedanaGPUServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to CedanaGPUServer will
// result in compilation errors.
type UnsafeCedanaGPUServer interface {
	mustEmbedUnimplementedCedanaGPUServer()
}

func RegisterCedanaGPUServer(s grpc.ServiceRegistrar, srv CedanaGPUServer) {
	s.RegisterService(&CedanaGPU_ServiceDesc, srv)
}

func _CedanaGPU_Checkpoint_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CheckpointRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CedanaGPUServer).Checkpoint(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedanagpu.CedanaGPU/Checkpoint",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CedanaGPUServer).Checkpoint(ctx, req.(*CheckpointRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CedanaGPU_Restore_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RestoreRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CedanaGPUServer).Restore(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedanagpu.CedanaGPU/Restore",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CedanaGPUServer).Restore(ctx, req.(*RestoreRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CedanaGPU_StartupPoll_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartupPollRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CedanaGPUServer).StartupPoll(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedanagpu.CedanaGPU/StartupPoll",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CedanaGPUServer).StartupPoll(ctx, req.(*StartupPollRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CedanaGPU_HealthCheck_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CedanaGPUServer).HealthCheck(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedanagpu.CedanaGPU/HealthCheck",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CedanaGPUServer).HealthCheck(ctx, req.(*HealthCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// CedanaGPU_ServiceDesc is the grpc.ServiceDesc for CedanaGPU service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var CedanaGPU_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "cedanagpu.CedanaGPU",
	HandlerType: (*CedanaGPUServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Checkpoint",
			Handler:    _CedanaGPU_Checkpoint_Handler,
		},
		{
			MethodName: "Restore",
			Handler:    _CedanaGPU_Restore_Handler,
		},
		{
			MethodName: "StartupPoll",
			Handler:    _CedanaGPU_StartupPoll_Handler,
		},
		{
			MethodName: "HealthCheck",
			Handler:    _CedanaGPU_HealthCheck_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "gpu.proto",
}
