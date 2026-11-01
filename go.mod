module github.com/aeryx-ai/sock-contract

go 1.26.2

require (
	connectrpc.com/connect v1.21.0
	github.com/google/uuid v1.6.0
	google.golang.org/protobuf v1.36.12
)

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
