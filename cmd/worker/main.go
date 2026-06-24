package main

import (
	"context"
	"egy-go-adk-app/applications"
	"egy-go-adk-app/services/slackmsg"
	"egy-go-adk-app/utilities"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
)

// worker is built ONCE per cold start (init) and reused across warm invocations so the
// expensive model/tool wiring is not repeated on every message.
var worker *applications.QueueWorker

func init() {
	utilities.InitializeLog()

	httpClient := applications.DefaultHTTPClient()

	runner, err := applications.BuildAgentRunner(context.Background(), httpClient)
	if err != nil {
		log.Fatalf("failed to build agent runner: %v", err)
	}

	worker = &applications.QueueWorker{
		AgentRunner: runner,
		ChatManager: applications.NewSlackManager(httpClient),
		// No QueueManager: the worker is invoked per-event by Lambda, not polled.
	}
}

// handle receives the raw Slack event envelope forwarded verbatim by the receiver
// Lambda (async invoke). All request parsing — email construction, command shaping,
// thread-history fetch — happens here in Go; the receiver carries no business logic.
func handle(ctx context.Context, envelope adkchatsdk.SlackEventEnvelope) error {
	msg := slackmsg.BuildCommandMessage(worker.ChatManager, envelope)
	worker.Process(ctx, msg)
	return nil
}

func main() {
	lambda.Start(handle)
}
