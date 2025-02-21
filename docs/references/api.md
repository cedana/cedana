## API reference

The Cedana daemon exposes a gRPC interface. Please check [daemon.proto](https://github.com/cedana/cedana-api/blob/940a0bdb105caa782d8151741065aa808c3e4b30/cedana/daemon/daemon.proto).

The API is under development and currently unstable. 

### SDK

For Go, we export a friendly [client package](https://github.com/cedana/cedana/tree/d618239b6052cda14f2117123414f8054f2d47ba/pkg/client), which has good defaults.

For other languages, you can directly import generated SDKs from our [Buf respository](https://buf.build/cedana/cedana/sdks/main:protobuf).
