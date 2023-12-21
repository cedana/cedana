// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v4.23.4
// source: task.proto

package task

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

// TaskServiceClient is the client API for TaskService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type TaskServiceClient interface {
	Dump(ctx context.Context, in *DumpArgs, opts ...grpc.CallOption) (*DumpResp, error)
	Restore(ctx context.Context, in *RestoreArgs, opts ...grpc.CallOption) (*RestoreResp, error)
	ContainerDump(ctx context.Context, in *ContainerDumpArgs, opts ...grpc.CallOption) (*ContainerDumpResp, error)
	ContainerRestore(ctx context.Context, in *ContainerRestoreArgs, opts ...grpc.CallOption) (*ContainerRestoreResp, error)
	RuncDump(ctx context.Context, in *RuncDumpArgs, opts ...grpc.CallOption) (*RuncDumpResp, error)
	RuncRestore(ctx context.Context, in *RuncRestoreArgs, opts ...grpc.CallOption) (*RuncRestoreResp, error)
	StartTask(ctx context.Context, in *StartTaskArgs, opts ...grpc.CallOption) (*StartTaskResp, error)
	LogStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_LogStreamingClient, error)
	ClientStateStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_ClientStateStreamingClient, error)
	MetaStateStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_MetaStateStreamingClient, error)
	ListRuncContainers(ctx context.Context, in *RuncRoot, opts ...grpc.CallOption) (*RuncList, error)
	GetRuncContainerByName(ctx context.Context, in *CtrByNameArgs, opts ...grpc.CallOption) (*CtrByNameResp, error)
}

type taskServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTaskServiceClient(cc grpc.ClientConnInterface) TaskServiceClient {
	return &taskServiceClient{cc}
}

func (c *taskServiceClient) Dump(ctx context.Context, in *DumpArgs, opts ...grpc.CallOption) (*DumpResp, error) {
	out := new(DumpResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/Dump", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) Restore(ctx context.Context, in *RestoreArgs, opts ...grpc.CallOption) (*RestoreResp, error) {
	out := new(RestoreResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/Restore", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ContainerDump(ctx context.Context, in *ContainerDumpArgs, opts ...grpc.CallOption) (*ContainerDumpResp, error) {
	out := new(ContainerDumpResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/ContainerDump", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) ContainerRestore(ctx context.Context, in *ContainerRestoreArgs, opts ...grpc.CallOption) (*ContainerRestoreResp, error) {
	out := new(ContainerRestoreResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/ContainerRestore", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) RuncDump(ctx context.Context, in *RuncDumpArgs, opts ...grpc.CallOption) (*RuncDumpResp, error) {
	out := new(RuncDumpResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/RuncDump", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) RuncRestore(ctx context.Context, in *RuncRestoreArgs, opts ...grpc.CallOption) (*RuncRestoreResp, error) {
	out := new(RuncRestoreResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/RuncRestore", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) StartTask(ctx context.Context, in *StartTaskArgs, opts ...grpc.CallOption) (*StartTaskResp, error) {
	out := new(StartTaskResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/StartTask", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) LogStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_LogStreamingClient, error) {
	stream, err := c.cc.NewStream(ctx, &TaskService_ServiceDesc.Streams[0], "/cedana.services.task.TaskService/LogStreaming", opts...)
	if err != nil {
		return nil, err
	}
	x := &taskServiceLogStreamingClient{stream}
	return x, nil
}

type TaskService_LogStreamingClient interface {
	Send(*LogStreamingResp) error
	Recv() (*LogStreamingArgs, error)
	grpc.ClientStream
}

type taskServiceLogStreamingClient struct {
	grpc.ClientStream
}

func (x *taskServiceLogStreamingClient) Send(m *LogStreamingResp) error {
	return x.ClientStream.SendMsg(m)
}

func (x *taskServiceLogStreamingClient) Recv() (*LogStreamingArgs, error) {
	m := new(LogStreamingArgs)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *taskServiceClient) ClientStateStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_ClientStateStreamingClient, error) {
	stream, err := c.cc.NewStream(ctx, &TaskService_ServiceDesc.Streams[1], "/cedana.services.task.TaskService/ClientStateStreaming", opts...)
	if err != nil {
		return nil, err
	}
	x := &taskServiceClientStateStreamingClient{stream}
	return x, nil
}

type TaskService_ClientStateStreamingClient interface {
	Send(*ClientStateStreamingResp) error
	Recv() (*ProcessState, error)
	grpc.ClientStream
}

type taskServiceClientStateStreamingClient struct {
	grpc.ClientStream
}

func (x *taskServiceClientStateStreamingClient) Send(m *ClientStateStreamingResp) error {
	return x.ClientStream.SendMsg(m)
}

func (x *taskServiceClientStateStreamingClient) Recv() (*ProcessState, error) {
	m := new(ProcessState)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *taskServiceClient) MetaStateStreaming(ctx context.Context, opts ...grpc.CallOption) (TaskService_MetaStateStreamingClient, error) {
	stream, err := c.cc.NewStream(ctx, &TaskService_ServiceDesc.Streams[2], "/cedana.services.task.TaskService/MetaStateStreaming", opts...)
	if err != nil {
		return nil, err
	}
	x := &taskServiceMetaStateStreamingClient{stream}
	return x, nil
}

type TaskService_MetaStateStreamingClient interface {
	Send(*MetaStateStreamingArgs) error
	Recv() (*MetaStateStreamingResp, error)
	grpc.ClientStream
}

type taskServiceMetaStateStreamingClient struct {
	grpc.ClientStream
}

func (x *taskServiceMetaStateStreamingClient) Send(m *MetaStateStreamingArgs) error {
	return x.ClientStream.SendMsg(m)
}

func (x *taskServiceMetaStateStreamingClient) Recv() (*MetaStateStreamingResp, error) {
	m := new(MetaStateStreamingResp)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *taskServiceClient) ListRuncContainers(ctx context.Context, in *RuncRoot, opts ...grpc.CallOption) (*RuncList, error) {
	out := new(RuncList)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/ListRuncContainers", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *taskServiceClient) GetRuncContainerByName(ctx context.Context, in *CtrByNameArgs, opts ...grpc.CallOption) (*CtrByNameResp, error) {
	out := new(CtrByNameResp)
	err := c.cc.Invoke(ctx, "/cedana.services.task.TaskService/GetRuncContainerByName", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TaskServiceServer is the server API for TaskService service.
// All implementations must embed UnimplementedTaskServiceServer
// for forward compatibility
type TaskServiceServer interface {
	Dump(context.Context, *DumpArgs) (*DumpResp, error)
	Restore(context.Context, *RestoreArgs) (*RestoreResp, error)
	ContainerDump(context.Context, *ContainerDumpArgs) (*ContainerDumpResp, error)
	ContainerRestore(context.Context, *ContainerRestoreArgs) (*ContainerRestoreResp, error)
	RuncDump(context.Context, *RuncDumpArgs) (*RuncDumpResp, error)
	RuncRestore(context.Context, *RuncRestoreArgs) (*RuncRestoreResp, error)
	StartTask(context.Context, *StartTaskArgs) (*StartTaskResp, error)
	LogStreaming(TaskService_LogStreamingServer) error
	ClientStateStreaming(TaskService_ClientStateStreamingServer) error
	MetaStateStreaming(TaskService_MetaStateStreamingServer) error
	ListRuncContainers(context.Context, *RuncRoot) (*RuncList, error)
	GetRuncContainerByName(context.Context, *CtrByNameArgs) (*CtrByNameResp, error)
	mustEmbedUnimplementedTaskServiceServer()
}

// UnimplementedTaskServiceServer must be embedded to have forward compatible implementations.
type UnimplementedTaskServiceServer struct {
}

func (UnimplementedTaskServiceServer) Dump(context.Context, *DumpArgs) (*DumpResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Dump not implemented")
}
func (UnimplementedTaskServiceServer) Restore(context.Context, *RestoreArgs) (*RestoreResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Restore not implemented")
}
func (UnimplementedTaskServiceServer) ContainerDump(context.Context, *ContainerDumpArgs) (*ContainerDumpResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContainerDump not implemented")
}
func (UnimplementedTaskServiceServer) ContainerRestore(context.Context, *ContainerRestoreArgs) (*ContainerRestoreResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ContainerRestore not implemented")
}
func (UnimplementedTaskServiceServer) RuncDump(context.Context, *RuncDumpArgs) (*RuncDumpResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RuncDump not implemented")
}
func (UnimplementedTaskServiceServer) RuncRestore(context.Context, *RuncRestoreArgs) (*RuncRestoreResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RuncRestore not implemented")
}
func (UnimplementedTaskServiceServer) StartTask(context.Context, *StartTaskArgs) (*StartTaskResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StartTask not implemented")
}
func (UnimplementedTaskServiceServer) LogStreaming(TaskService_LogStreamingServer) error {
	return status.Errorf(codes.Unimplemented, "method LogStreaming not implemented")
}
func (UnimplementedTaskServiceServer) ClientStateStreaming(TaskService_ClientStateStreamingServer) error {
	return status.Errorf(codes.Unimplemented, "method ClientStateStreaming not implemented")
}
func (UnimplementedTaskServiceServer) MetaStateStreaming(TaskService_MetaStateStreamingServer) error {
	return status.Errorf(codes.Unimplemented, "method MetaStateStreaming not implemented")
}
func (UnimplementedTaskServiceServer) ListRuncContainers(context.Context, *RuncRoot) (*RuncList, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListRuncContainers not implemented")
}
func (UnimplementedTaskServiceServer) GetRuncContainerByName(context.Context, *CtrByNameArgs) (*CtrByNameResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetRuncContainerByName not implemented")
}
func (UnimplementedTaskServiceServer) mustEmbedUnimplementedTaskServiceServer() {}

// UnsafeTaskServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to TaskServiceServer will
// result in compilation errors.
type UnsafeTaskServiceServer interface {
	mustEmbedUnimplementedTaskServiceServer()
}

func RegisterTaskServiceServer(s grpc.ServiceRegistrar, srv TaskServiceServer) {
	s.RegisterService(&TaskService_ServiceDesc, srv)
}

func _TaskService_Dump_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DumpArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).Dump(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/Dump",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).Dump(ctx, req.(*DumpArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_Restore_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RestoreArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).Restore(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/Restore",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).Restore(ctx, req.(*RestoreArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ContainerDump_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ContainerDumpArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ContainerDump(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/ContainerDump",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ContainerDump(ctx, req.(*ContainerDumpArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_ContainerRestore_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ContainerRestoreArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ContainerRestore(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/ContainerRestore",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ContainerRestore(ctx, req.(*ContainerRestoreArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_RuncDump_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RuncDumpArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).RuncDump(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/RuncDump",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).RuncDump(ctx, req.(*RuncDumpArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_RuncRestore_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RuncRestoreArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).RuncRestore(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/RuncRestore",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).RuncRestore(ctx, req.(*RuncRestoreArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_StartTask_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartTaskArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).StartTask(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/StartTask",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).StartTask(ctx, req.(*StartTaskArgs))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_LogStreaming_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(TaskServiceServer).LogStreaming(&taskServiceLogStreamingServer{stream})
}

type TaskService_LogStreamingServer interface {
	Send(*LogStreamingArgs) error
	Recv() (*LogStreamingResp, error)
	grpc.ServerStream
}

type taskServiceLogStreamingServer struct {
	grpc.ServerStream
}

func (x *taskServiceLogStreamingServer) Send(m *LogStreamingArgs) error {
	return x.ServerStream.SendMsg(m)
}

func (x *taskServiceLogStreamingServer) Recv() (*LogStreamingResp, error) {
	m := new(LogStreamingResp)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _TaskService_ClientStateStreaming_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(TaskServiceServer).ClientStateStreaming(&taskServiceClientStateStreamingServer{stream})
}

type TaskService_ClientStateStreamingServer interface {
	Send(*ProcessState) error
	Recv() (*ClientStateStreamingResp, error)
	grpc.ServerStream
}

type taskServiceClientStateStreamingServer struct {
	grpc.ServerStream
}

func (x *taskServiceClientStateStreamingServer) Send(m *ProcessState) error {
	return x.ServerStream.SendMsg(m)
}

func (x *taskServiceClientStateStreamingServer) Recv() (*ClientStateStreamingResp, error) {
	m := new(ClientStateStreamingResp)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _TaskService_MetaStateStreaming_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(TaskServiceServer).MetaStateStreaming(&taskServiceMetaStateStreamingServer{stream})
}

type TaskService_MetaStateStreamingServer interface {
	Send(*MetaStateStreamingResp) error
	Recv() (*MetaStateStreamingArgs, error)
	grpc.ServerStream
}

type taskServiceMetaStateStreamingServer struct {
	grpc.ServerStream
}

func (x *taskServiceMetaStateStreamingServer) Send(m *MetaStateStreamingResp) error {
	return x.ServerStream.SendMsg(m)
}

func (x *taskServiceMetaStateStreamingServer) Recv() (*MetaStateStreamingArgs, error) {
	m := new(MetaStateStreamingArgs)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func _TaskService_ListRuncContainers_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RuncRoot)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).ListRuncContainers(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/ListRuncContainers",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).ListRuncContainers(ctx, req.(*RuncRoot))
	}
	return interceptor(ctx, in, info, handler)
}

func _TaskService_GetRuncContainerByName_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CtrByNameArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TaskServiceServer).GetRuncContainerByName(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cedana.services.task.TaskService/GetRuncContainerByName",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TaskServiceServer).GetRuncContainerByName(ctx, req.(*CtrByNameArgs))
	}
	return interceptor(ctx, in, info, handler)
}

// TaskService_ServiceDesc is the grpc.ServiceDesc for TaskService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var TaskService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "cedana.services.task.TaskService",
	HandlerType: (*TaskServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Dump",
			Handler:    _TaskService_Dump_Handler,
		},
		{
			MethodName: "Restore",
			Handler:    _TaskService_Restore_Handler,
		},
		{
			MethodName: "ContainerDump",
			Handler:    _TaskService_ContainerDump_Handler,
		},
		{
			MethodName: "ContainerRestore",
			Handler:    _TaskService_ContainerRestore_Handler,
		},
		{
			MethodName: "RuncDump",
			Handler:    _TaskService_RuncDump_Handler,
		},
		{
			MethodName: "RuncRestore",
			Handler:    _TaskService_RuncRestore_Handler,
		},
		{
			MethodName: "StartTask",
			Handler:    _TaskService_StartTask_Handler,
		},
		{
			MethodName: "ListRuncContainers",
			Handler:    _TaskService_ListRuncContainers_Handler,
		},
		{
			MethodName: "GetRuncContainerByName",
			Handler:    _TaskService_GetRuncContainerByName_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "LogStreaming",
			Handler:       _TaskService_LogStreaming_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
		{
			StreamName:    "ClientStateStreaming",
			Handler:       _TaskService_ClientStateStreaming_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
		{
			StreamName:    "MetaStateStreaming",
			Handler:       _TaskService_MetaStateStreaming_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "task.proto",
}
