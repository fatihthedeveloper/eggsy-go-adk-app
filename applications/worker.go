package applications

import (
	"context"
	"egy-go-adk-app/services/agentrunner"
	"egy-go-adk-app/services/queue"
	"log/slog"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"google.golang.org/genai"
)

// QueueWorker runs a single job: react to the Slack message, run the agent, and reply.
// It used to also own a polling loop (Run); under Lambda each message is delivered as a
// discrete invocation, so the loop is gone and Process is called once per invocation.
type QueueWorker struct {
	AgentRunner *agentrunner.EphemeralRunner
	ChatManager adkchatsdk.ChatManager
}

func (q *QueueWorker) Process(ctx context.Context, msg queue.QueueMessage) {
	// extract attributes
	id, user, sessionId := msg.Id, msg.Body["user"], msg.Body["sessionId"]
	command, messageId, channelId := msg.Body["command"], msg.Body["messageId"], msg.Body["channelId"]
	slog.InfoContext(ctx, "executing process w. "+id)

	// set loading emoji
	reactionReqBuilder := adkchatsdk.SlackReactToMessageRequestBuilder{
		ChannelId:        channelId,
		ThreadTimestamp:  sessionId,
		MessageTimestamp: messageId,
		Emoji:            "keyboard-work",
	}
	q.ChatManager.ReactToMessage(reactionReqBuilder.Build())

	// run agent (session is auto-deleted by the ephemeral runner)
	finalText, err := q.AgentRunner.Run(ctx, user, sessionId, &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{{Text: command}},
	})
	if err != nil {
		slog.ErrorContext(ctx, "agent error", "err", err)
		return
	}

	// log response
	slog.InfoContext(ctx, "agent final response",
		"userId", user,
		"sessionId", sessionId,
		"response", finalText,
	)

	// unset loading emoji
	for range 5 {
		e := q.ChatManager.UnreactToMessage(reactionReqBuilder.Build())
		if e != nil {
			slog.ErrorContext(ctx, "error: "+e.Error())
			return
		} else {
			break
		}
	}

	// reply in slack
	replyBuilder := adkchatsdk.SlackReplyToMessageRequestBuilder{
		ChannelId:       channelId,
		ThreadTimestamp: sessionId,
		MarkdownText:    finalText,
		Emoji:           "tennis",
	}
	for range 5 {
		e := q.ChatManager.ReplyToMessage(replyBuilder.Build())
		if e != nil {
			slog.ErrorContext(ctx, "error: "+e.Error())
		} else {
			break
		}
	}
}
