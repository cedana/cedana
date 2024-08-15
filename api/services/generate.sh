#/bin/bash

# Use this to generate GPU and Task proto files after updating the submodules.
# You can update the submodule with git submodule update --init --recursive

PROTO_DIR="cedana-api"

# Generate Go code for gpu.proto
protoc --go_out=gpu --go_opt=paths=source_relative \
    --go-grpc_out=gpu --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    -I$PROTO_DIR \
    $PROTO_DIR/gpu.proto

# Generate Go code for task.proto
protoc --go_out=task --go_opt=paths=source_relative \
    --go-grpc_out=task --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go_opt=Mtask.proto=github.com/cedana/cedana/api/services/task \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go-grpc_opt=Mtask.proto=github.com/cedana/cedana/api/services/task \
    -I$PROTO_DIR \
    $PROTO_DIR/task.proto
