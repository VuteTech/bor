module github.com/VuteTech/Bor/agent

go 1.25.0

require (
	github.com/VuteTech/Bor/server v0.0.0
	github.com/fsnotify/fsnotify v1.9.0
	google.golang.org/grpc v1.79.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace github.com/VuteTech/Bor/server => ../server
