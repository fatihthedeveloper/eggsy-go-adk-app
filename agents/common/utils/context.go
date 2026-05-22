package utils

import (
	"context"

	"google.golang.org/adk/tool"
)

func NewContext(c tool.Context) context.Context {
	ctx := context.Background()

	ctx = context.WithValue(ctx, "sessionId", c.SessionID())
	ctx = context.WithValue(ctx, "userId", c.UserID())
	ctx = context.WithValue(ctx, "invocationId", c.InvocationID())
	ctx = context.WithValue(ctx, "appName", c.AppName())

	return ctx
}
