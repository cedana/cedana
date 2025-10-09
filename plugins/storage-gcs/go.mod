module github.com/cedana/cedana/plugins/storage-gcs

go 1.25

replace github.com/cedana/cedana => ../..

require github.com/cedana/cedana v0.0.0

require (
	buf.build/gen/go/cedana/cedana/protocolbuffers/go v1.36.10-20251009084235-3942f1f92d9c.1 // indirect
	buf.build/gen/go/cedana/criu/protocolbuffers/go v1.36.10-20251009084235-2127b839c830.1 // indirect
	github.com/frankban/quicktest v1.14.6 // indirect
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	golang.org/x/net v0.46.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250825161204-c5933d9347a5 // indirect
	google.golang.org/grpc v1.76.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
