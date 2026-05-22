package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/queues"
)

type AppQueue int

const (
	Tasks AppQueue = iota
)

type CloudflareApplication struct {
	Client    *cloudflare.Client
	AccountId string
}

type CloudflareQueueManager struct {
	CloudflareApp CloudflareApplication
}

func (cqm *CloudflareQueueManager) Map() map[AppQueue]string {
	return map[AppQueue]string{
		Tasks: "701071a23ff54372b0c359de1bb038ce",
	}
}

func (cqm *CloudflareQueueManager) Publish(ctx context.Context, queueName AppQueue, message QueueMessage) error {
	queueId := cqm.Map()[queueName]

	jsonData, err := json.Marshal(message.Body)
	if err != nil {
		slog.ErrorContext(ctx, "failed to serialze message")
		return errors.New("Message Serialization Error")
	}

	// 3. Convert []byte to string
	jsonStr := string(jsonData)

	_, e := cqm.CloudflareApp.Client.Queues.Messages.Push(ctx, queueId, queues.MessagePushParams{
		Body: queues.MessagePushParamsBodyMqQueueMessageText{
			Body:        cloudflare.F(jsonStr),
			ContentType: cloudflare.F(queues.MessagePushParamsBodyMqQueueMessageTextContentTypeText),
		},
		AccountID: cloudflare.F(cqm.CloudflareApp.AccountId),
	})

	if e != nil {
		return e
	}

	return nil
}

func (cqm *CloudflareQueueManager) Poll(ctx context.Context, queueName AppQueue, visibilityTimeout int, batchSize int) ([]QueueMessage, error) {
	queueId := cqm.Map()[queueName]

	msgs, e := cqm.CloudflareApp.Client.Queues.Messages.Pull(ctx, queueId, queues.MessagePullParams{
		BatchSize:           cloudflare.F(float64(batchSize)),
		VisibilityTimeoutMs: cloudflare.F(float64(visibilityTimeout)),
		AccountID:           cloudflare.F(cqm.CloudflareApp.AccountId),
	})

	if e != nil {
		return []QueueMessage{}, e
	}

	messages := []QueueMessage{}

	for _, msg := range msgs.Messages {
		var data map[string]string

		err := json.Unmarshal([]byte(msg.Body), &data)
		if err != nil {
			slog.ErrorContext(ctx, "failed to unmarshal queue message")
			continue
		}

		messages = append(messages, QueueMessage{
			Id:   msg.LeaseID,
			Body: data,
		})
	}

	return messages, nil
}

func (cqm *CloudflareQueueManager) Ack(ctx context.Context, queueName AppQueue, messages []QueueMessage) error {
	queueId := cqm.Map()[queueName]

	acks := []queues.MessageAckParamsAck{}

	for _, msg := range messages {
		acks = append(acks, queues.MessageAckParamsAck{
			LeaseID: cloudflare.F(msg.Id),
		})
	}

	_, e := cqm.CloudflareApp.Client.Queues.Messages.Ack(ctx, queueId, queues.MessageAckParams{
		Acks:      cloudflare.F(acks),
		AccountID: cloudflare.F(cqm.CloudflareApp.AccountId),
	})

	if e != nil {
		return e
	}

	return nil
}
