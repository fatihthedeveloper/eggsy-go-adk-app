package queue

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type inMemoryMessage struct {
	message        QueueMessage
	invisibleUntil time.Time
}

type InMemoryQueueManager struct {
	mu     sync.Mutex
	queues map[AppQueue][]*inMemoryMessage
}

func NewInMemoryQueueManager() QueueManager {
	return &InMemoryQueueManager{
		queues: make(map[AppQueue][]*inMemoryMessage),
	}
}

func (m *InMemoryQueueManager) Map() map[AppQueue]string {
	return map[AppQueue]string{
		Tasks: "tasks",
	}
}

func (m *InMemoryQueueManager) Publish(_ context.Context, queueName AppQueue, body QueueMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if body.Id == "" {
		body.Id = fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
	}

	m.queues[queueName] = append(m.queues[queueName], &inMemoryMessage{
		message: body,
	})
	return nil
}

func (m *InMemoryQueueManager) Poll(_ context.Context, queueName AppQueue, visibilityTimeout int, batchSize int) ([]QueueMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var result []QueueMessage

	for _, msg := range m.queues[queueName] {
		if len(result) >= batchSize {
			break
		}
		if now.After(msg.invisibleUntil) {
			msg.invisibleUntil = now.Add(time.Duration(visibilityTimeout) * time.Millisecond)
			result = append(result, msg.message)
		}
	}

	return result, nil
}

func (m *InMemoryQueueManager) Ack(_ context.Context, queueName AppQueue, messages []QueueMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ackIds := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		ackIds[msg.Id] = struct{}{}
	}

	queue := m.queues[queueName]
	filtered := queue[:0]
	for _, msg := range queue {
		if _, ok := ackIds[msg.message.Id]; !ok {
			filtered = append(filtered, msg)
		}
	}
	m.queues[queueName] = filtered

	return nil
}
