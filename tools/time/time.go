package time

import (
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type GetTimeArgs struct{}

type TimeInfo struct {
	CurrentTime string `json:"current_time"`
	TimeZone    string `json:"timezone"`
}

// MakeGetTimeTool returns a reusable ADK tool that retrieves the current system time.
func MakeGetTimeTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_time",
		Description: "Retrieves the current local system date, time, and timezone. Use this when the user asks about the time or date.",
	}, func(ctx tool.Context, args GetTimeArgs) (TimeInfo, error) {
		now := time.Now()
		zone, _ := now.Zone()
		return TimeInfo{
			CurrentTime: now.Format(time.RFC1123),
			TimeZone:    zone,
		}, nil
	})
}
