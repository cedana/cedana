# Use this to generate from proto files after updating the submodules.
# You can update the submodule with git submodule update --init --recursive

PROTO_DIR="proto"

# Generate Go code for gpu.proto
mkdir -p gpu
protoc --go_out=gpu --go_opt=paths=source_relative \
    --go-grpc_out=gpu --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/gpu \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/gpu \
    -I$PROTO_DIR \
    $PROTO_DIR/gpu.proto

# Generate Go code for daemon.proto
mkdir -p daemon
protoc --go_out=daemon --go_opt=paths=source_relative \
    --go-grpc_out=daemon --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/gpu \
    --go_opt=Mdaemon.proto=github.com/cedana/cedana/pkg/api/daemon \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/pkg/api/gpu \
    --go-grpc_opt=Mdaemon.proto=github.com/cedana/cedana/pkg/api/daemon \
    -I$PROTO_DIR \
    $PROTO_DIR/daemon.proto --experimental_allow_proto3_optional
