package slackmsg

import (
	"egy-go-adk-app/services/queue"
	"encoding/json"
	"fmt"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"github.com/google/uuid"
)

// BuildCommandMessage converts a raw Slack event envelope into the QueueMessage the
// worker processes. When the message belongs to a thread it fetches the thread history
// so the agent has conversational context.
//
// This is the single mapping from a Slack event to the worker's job payload. It is
// shared by the local-dev HTTP controller (which publishes the message to a queue) and
// the Lambda worker handler (which builds the message from the forwarded event and runs
// it directly), so the two paths can never drift apart.
func BuildCommandMessage(chatMgr adkchatsdk.ChatManager, envelope adkchatsdk.SlackEventEnvelope) queue.QueueMessage {
	messageId, sessionId := envelope.Event.TS, envelope.Event.TS

	threadHistory := "[]"
	if envelope.Event.ThreadTS != "" {
		sessionId = envelope.Event.ThreadTS

		threadHistoryRawData, _ := chatMgr.FetchThread(adkchatsdk.FetchThreadRequest{
			ChannelId: envelope.Event.Channel,
			ThreadId:  sessionId,
		})

		jsonThreadHistory, _ := json.Marshal(threadHistoryRawData)
		threadHistory = string(jsonThreadHistory)
	}

	newEmail := fmt.Sprintf("%s@kresnofatihimani.slack.com", envelope.Event.User)

	cmd, _ := json.Marshal(map[string]string{
		"UserEmail":     newEmail,
		"Prompt":        envelope.Event.Text,
		"ThreadHistory": threadHistory,
	})

	return queue.QueueMessage{
		Id: uuid.NewString(),
		Body: map[string]string{
			"user":      envelope.Event.User,
			"sessionId": sessionId,
			"command":   string(cmd),
			"messageId": messageId,
			"channelId": envelope.Event.Channel,
		},
	}
}
