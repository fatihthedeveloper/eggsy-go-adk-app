package tools

import "google.golang.org/adk/tool"

type CommonToolsBuilder interface {
	GetUTCISOTimestampTool() (tool.Tool, error)
}

type NativeImplCommonToolsBuilder struct{}
