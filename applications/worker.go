package applications

import (
	"context"
	"egy-go-adk-app/services/agentrunner"
	"egy-go-adk-app/services/queue"
	"log/slog"
	"sync"
	"time"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"google.golang.org/genai"
)

type QueueWorker struct {
	QueueManager queue.QueueManager
	AgentRunner  *agentrunner.EphemeralRunner
	ChatManager  adkchatsdk.ChatManager
}

func (q *QueueWorker) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		// Sleep interruptibly: wake up either after 5s or when ctx is cancelled.
		select {
		case <-ctx.Done():
			slog.Info("Background worker shutting down...")
			return
		case <-time.After(time.Second * 5):
			cont := context.Background()
			msgs, er := q.QueueManager.Poll(cont, queue.Tasks, 10, 10)
			if er != nil {
				slog.ErrorContext(cont, "Failed to poll", "error", er.Error())
				return
			}

			for _, msg := range msgs {
				slog.InfoContext(cont, "fetched msg with data:"+msg.Body["id"])

				q.Process(cont, msg)
				q.QueueManager.Ack(cont, queue.Tasks, []queue.QueueMessage{msg})

				slog.InfoContext(cont, "acked msg with data:"+msg.Body["id"])
			}
		}
	}
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
