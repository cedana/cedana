#/bin/bash

# Use this to generate from proto files after updating the submodules.
# You can update the submodule with git submodule update --init --recursive

PROTO_DIR="cedana-api"

# Generate Go code for gpu.proto
protoc --go_out=comm --go_opt=paths=source_relative \
    --go-grpc_out=comm --go-grpc_opt=paths=source_relative \
    --go_opt=Mcomm.proto=github.com/cedana/cedana/api/services/comm \
    --go-grpc_opt=Mcomm.proto=github.com/cedana/cedana/api/services/comm \
    -I$PROTO_DIR \
    $PROTO_DIR/comm.proto

# Generate Go code for gpu.proto
protoc --go_out=gpu --go_opt=paths=source_relative \
    --go-grpc_out=gpu --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    -I$PROTO_DIR \
    $PROTO_DIR/gpu.proto

# Generate Go code for image.proto
protoc --go_out=image --go_opt=paths=source_relative \
    --go-grpc_out=image --go-grpc_opt=paths=source_relative \
    --go_opt=Mimage.proto=github.com/cedana/cedana/api/services/image \
    --go-grpc_opt=Mimage.proto=github.com/cedana/cedana/api/services/image \
    -I$PROTO_DIR \
    $PROTO_DIR/image.proto

# Generate Go code for img-streamer.proto
protoc --go_out=img_streamer --go_opt=paths=source_relative \
    --go-grpc_out=img_streamer --go-grpc_opt=paths=source_relative \
    --go_opt=Mimg_streamer.proto=github.com/cedana/cedana/api/services/img-streamer \
    --go-grpc_opt=Mimg_streamer.proto=github.com/cedana/cedana/api/services/img-streamer \
    -I$PROTO_DIR \
    $PROTO_DIR/img-streamer.proto

# Generate Go code for rpc.proto
protoc --go_out=rpc --go_opt=paths=source_relative \
    --go-grpc_out=rpc --go-grpc_opt=paths=source_relative \
    --go_opt=Mrpc.proto=github.com/cedana/cedana/api/services/rpc \
    --go-grpc_opt=Mrpc.proto=github.com/cedana/cedana/api/services/rpc \
    -I$PROTO_DIR \
    $PROTO_DIR/rpc.proto

# Generate Go code for task.proto
protoc --go_out=task --go_opt=paths=source_relative \
    --go-grpc_out=task --go-grpc_opt=paths=source_relative \
    --go_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go_opt=Mtask.proto=github.com/cedana/cedana/api/services/task \
    --go-grpc_opt=Mgpu.proto=github.com/cedana/cedana/api/services/gpu \
    --go-grpc_opt=Mtask.proto=github.com/cedana/cedana/api/services/task \
    -I$PROTO_DIR \
    $PROTO_DIR/task.proto
