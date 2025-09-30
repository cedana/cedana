## API reference

### gRPC

The Cedana daemon exposes a gRPC interface. Please check [daemon.proto](https://github.com/cedana/cedana-api/blob/main/cedana/daemon/daemon.proto). The CLI is simply a client of the daemon and uses this API.

### SDK

#### Golang

For Go, we export a friendly [client package](https://github.com/cedana/cedana/tree/main/pkg/client), which has good defaults.

#### Other languages

For other languages, you can directly import SDKs from our [Buf respository](https://buf.build/cedana/cedana/sdks/main:protobuf).
