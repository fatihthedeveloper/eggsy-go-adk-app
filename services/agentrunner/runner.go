package agentrunner

import (
	"context"
	"log/slog"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type EphemeralRunner struct {
	AppName        string
	Runner         *runner.Runner
	SessionService session.Service
}

func (e *EphemeralRunner) Run(ctx context.Context, userId, sessionId string, content *genai.Content) (string, error) {
	defer func() {
		err := e.SessionService.Delete(ctx, &session.DeleteRequest{
			AppName:   e.AppName,
			UserID:    userId,
			SessionID: sessionId,
		})
		if err != nil {
			slog.WarnContext(ctx, "ephemeral session delete failed", "err", err, "sessionId", sessionId)
		}
	}()

	var finalText string
	for event, err := range e.Runner.Run(ctx, userId, sessionId, content, agent.RunConfig{}) {
		if err != nil {
			return finalText, err
		}
		if event == nil || event.LLMResponse.Partial || event.LLMResponse.Content == nil {
			continue
		}
		for _, p := range event.LLMResponse.Content.Parts {
			if p.Text != "" {
				finalText = p.Text
			}
		}
	}
	return finalText, nil
}
