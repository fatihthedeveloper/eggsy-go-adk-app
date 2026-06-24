package main

import (
	"context"
	"egy-go-adk-app/applications"
	"egy-go-adk-app/controllers"
	"egy-go-adk-app/services/queue"
	"egy-go-adk-app/utilities"
	"log"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// This is the LOCAL-DEVELOPMENT entry point: it runs the HTTP server, an in-memory
// queue, and a polling worker loop in a single process — the old Lightsail container
// model. Production runs on AWS Lambda via cmd/worker (the agent) behind the Python
// receiver Lambda (the HTTP front door). See LAMBDA_MIGRATION.md.
func app() {
	utilities.InitializeLog()

	slog.Info("Initializing local dev app!")

	// ctx is cancelled automatically when SIGINT or SIGTERM is received.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpClient := applications.DefaultHTTPClient()

	agentRunner, err := applications.BuildAgentRunner(ctx, httpClient)
	if err != nil {
		log.Fatalf("failed to build agent runner: %v", err)
	}

	slackMgr := applications.NewSlackManager(httpClient)
	queueMgr := queue.NewInMemoryQueueManager()

	httpServer := applications.HttpServer{
		Controllers: []controllers.Controller{
			&controllers.HealthController{},
			&controllers.SlackWebhookController{
				QueueManager: queueMgr,
				ChatManager:  slackMgr,
			},
		},
	}

	worker := &applications.QueueWorker{
		AgentRunner: agentRunner,
		ChatManager: slackMgr,
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go httpServer.Run(ctx, &wg)
	go runLocalWorkerLoop(ctx, &wg, queueMgr, worker)

	slog.Info("Initialized local dev app!")

	wg.Wait()

	slog.Info("Exiting App!")
}

// runLocalWorkerLoop replaces the QueueWorker.Run poll loop that was removed for Lambda.
// It exists only for local development against the in-memory queue.
func runLocalWorkerLoop(ctx context.Context, wg *sync.WaitGroup, queueMgr queue.QueueManager, worker *applications.QueueWorker) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Background worker shutting down...")
			return
		case <-time.After(time.Second * 2):
			cont := context.Background()
			msgs, er := queueMgr.Poll(cont, queue.Tasks, 30000, 10)
			if er != nil {
				slog.ErrorContext(cont, "Failed to poll", "error", er.Error())
				continue
			}

			for _, msg := range msgs {
				worker.Process(cont, msg)
				queueMgr.Ack(cont, queue.Tasks, []queue.QueueMessage{msg})
			}
		}
	}
}

func main() {
	app()
}
