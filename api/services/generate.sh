#/bin/bash

# Use this to generate GPU and Task proto files after updating the submodules.
# You can update the submodule with git submodule update --init --recursive

function generate_task_proto() {
    protoc --go_out=task --go_opt=paths=source_relative \
       --go-grpc_out=task --go-grpc_opt=paths=source_relative \
       -I cedana-api \
       cedana-api/task.proto
}


function generate_gpu_proto() {
    protoc --go_out=gpu --go_opt=paths=source_relative \
       --go-grpc_out=gpu --go-grpc_opt=paths=source_relative \
       -I cedana-api \
       cedana-api/gpu.proto
}

generate_task_proto
generate_gpu_proto
