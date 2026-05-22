package tools

import (
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type getUTCISOTimestampArgs struct{}

type getUTCISOTimestampResult struct {
	UTC_ISO_Timestamp string `json:"utc_iso_timestamp" jsonschema:"the result utc iso timestamp, e.g. 2006-01-02T15:04:05.000Z"`
}

func (n *NativeImplCommonToolsBuilder) GetUTCISOTimestampTool() (tool.Tool, error) {
	fn := func(ctx tool.Context, _ getUTCISOTimestampArgs) (getUTCISOTimestampResult, error) {
		return getUTCISOTimestampResult{
			UTC_ISO_Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		}, nil
	}

	newTool, err := functiontool.New(functiontool.Config{
		Name:        "get_current_utc_iso_timestamp_tool",
		Description: "Gets the correct current UTC ISO Timestamp string",
	}, fn)

	return newTool, err
}
