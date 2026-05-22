package queue

import "context"

type QueueMessage struct {
	Id   string
	Body map[string]string
}

type QueueManager interface {
	Publish(ctx context.Context, queueName AppQueue, body QueueMessage) error
	Poll(ctx context.Context, queueName AppQueue, visibilityTimeout int, batchSize int) ([]QueueMessage, error)
	Ack(ctx context.Context, queueName AppQueue, messages []QueueMessage) error
	Map() map[AppQueue]string
}
