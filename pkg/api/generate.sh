# Use this to generate from proto files after updating the submodules.
# You can update the submodule with git submodule update --init --recursive

PROTO_DIR=proto

mkdir -p plugins/gpu
protoc --go_out=plugins/gpu --go_opt=paths=source_relative \
    --go-grpc_out=plugins/gpu --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/plugins/gpu \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/plugins/gpu \
    -I$PROTO_DIR/plugins \
    $PROTO_DIR/plugins/gpu.proto

mkdir -p plugins/runc
protoc --go_out=plugins/runc --go_opt=paths=source_relative \
    --go-grpc_out=plugins/runc --go-grpc_opt=paths=source_relative \
    --go_opt=Mrunc.proto=github.com/cedana/cedana/pkg/api/plugins/runc \
    --go-grpc_opt=Mrunc.proto=github.com/cedana/cedana/pkg/api/plugins/runc \
    -I$PROTO_DIR/plugins \
    $PROTO_DIR/plugins/runc.proto

mkdir -p daemon
protoc --go_out=daemon --go_opt=paths=source_relative \
    --go-grpc_out=daemon --go-grpc_opt=paths=source_relative \
    --go_opt=Mdaemon.proto=github.com/cedana/cedana/pkg/api/daemon \
    --go-grpc_opt=Mdaemon.proto=github.com/cedana/cedana/pkg/api/daemon \
    --go_opt=Mplugins/runc.proto=github.com/cedana/cedana/pkg/api/plugins/runc \
    --go-grpc_opt=Mplugins/runc.proto=github.com/cedana/cedana/pkg/api/plugins/runc \
    -I$PROTO_DIR \
    $PROTO_DIR/daemon.proto --experimental_allow_proto3_optional
