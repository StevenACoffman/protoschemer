module github.com/StevenACoffman/protoschemer

go 1.27.0

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.2
	github.com/bufbuild/protocompile v0.14.1
	github.com/ettle/strcase v0.2.0
	github.com/jhump/protoreflect/v2 v2.0.0-beta.2
	github.com/peterbourgon/ff/v4 v4.0.0-beta.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/sudorandom/protoc-gen-connect-openapi v0.27.1
	github.com/swaggest/jsonschema-go v0.3.79
	google.golang.org/genproto v0.0.0-20260911204522-f61a6ca850bd
	google.golang.org/protobuf v1.36.12
	pgregory.net/rapid v1.3.0
)

require (
	buf.build/go/protovalidate v1.4.0 // indirect
	cel.dev/cel-go v0.32.0 // indirect
	cel.dev/expr v0.25.3 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/gobwas/glob v1.0.0 // indirect
	github.com/google/gnostic v0.7.1 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/lmittmann/tint v1.2.0 // indirect
	github.com/pb33f/jsonpath v0.8.3 // indirect
	github.com/pb33f/libopenapi v0.38.7 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/swaggest/refl v1.4.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/exp v0.0.0-20260824195058-e88cd73687aa // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260904194346-d0f1323225a4 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260904194346-d0f1323225a4 // indirect
)

replace github.com/sudorandom/protoc-gen-connect-openapi => github.com/StevenACoffman/protoc-gen-connect-openapi v0.0.0-20260912190720-3481e7ca1ce6
