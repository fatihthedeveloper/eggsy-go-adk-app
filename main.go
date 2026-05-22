package main

import (
	"context"
	commonTools "egy-go-adk-app/agents/common/tools"
	trxTools "egy-go-adk-app/agents/transactiontracker/tools"
	"egy-go-adk-app/applications"
	"egy-go-adk-app/controllers"
	"egy-go-adk-app/services/agentrunner"
	"egy-go-adk-app/services/queue"
	"egy-go-adk-app/utilities"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	commoncloudflared1sdk "github.com/fatihthedeveloper/cloudflare-d1-sdk"
	eggsyaccountsdk "github.com/fatihthedeveloper/eggsy-account-sdk"
	eggsytransactionsdk "github.com/fatihthedeveloper/eggsy-transaction-sdk"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const appName = "transactions_tracker_app"

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func app() {
	utilities.InitializeLog()

	slog.Info("Initializing App!")

	// ctx is cancelled automatically when SIGINT or SIGTERM is received.
	// stop() releases the signal resources — always defer it.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	httpClient := &http.Client{
		Timeout: 10 * time.Second, // Set a sensible timeout
	}

	cloudflareAccountId := mustEnv("CLOUDFLARE_ACCOUNT_ID")

	var queueMgr queue.QueueManager

	queueMgr = queue.NewInMemoryQueueManager()

	slackMgr := &adkchatsdk.SlackChatManager{
		SdkConfig: adkchatsdk.SlackSdkConfig{
			SigningSecret: mustEnv("SLACK_SIGNING_SECRET"),
			APIToken:      mustEnv("SLACK_API_TOKEN"),
		},
		Client: httpClient,
	}

	httpServer := applications.HttpServer{
		Controllers: []controllers.Controller{
			&controllers.HealthController{},
			&controllers.SlackWebhookController{
				QueueManager: queueMgr,
				ChatManager:  slackMgr,
				BypassToken:  mustEnv("SLACK_WEBHOOK_BYPASS_TOKEN"),
			},
		},
	}

	model, err := gemini.NewModel(ctx, "gemini-flash-latest", &genai.ClientConfig{
		APIKey: mustEnv("GEMINI_API_KEY"),
		// Project: "588749770102",
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	cloudflareConfig := &commoncloudflared1sdk.D1Config{
		ProjectId:  cloudflareAccountId,
		DatabaseId: mustEnv("CLOUDFLARE_D1_DATABASE_ID"),
		APIToken:   mustEnv("CLOUDFLARE_D1_API_TOKEN"),
	}

	transactionSvcBuilder := eggsytransactionsdk.CloudFlareD1TransactionServiceBuilder{
		HttpClient:       httpClient,
		CloudFlareConfig: cloudflareConfig,
	}
	transactionSvc, _ := transactionSvcBuilder.Build()

	accountSvcBuilder := eggsyaccountsdk.CloudFlareD1AccountServiceBuilder{
		HttpClient:       httpClient,
		CloudFlareConfig: cloudflareConfig,
	}
	accountSvc, _ := accountSvcBuilder.Build()

	trxToolsBuilder := trxTools.NativeImplTransactionTrackerToolsBuilder{
		TrxService: transactionSvc,
		AccService: accountSvc,
	}

	commonToolsBuilder := commonTools.NativeImplCommonToolsBuilder{}

	agentTools := []tool.Tool{}

	type toolSet struct {
		Tool  tool.Tool
		Error error
	}

	for _, toolFn := range []func() (tool.Tool, error){
		trxToolsBuilder.CreateAccountByEmailTool,
		trxToolsBuilder.GetAccountByEmailTool,
		trxToolsBuilder.CreateTransactionTool,
		trxToolsBuilder.ListTransactionTool,
		trxToolsBuilder.UpdateTransactionTool,
		trxToolsBuilder.DeleteTransactionTool,
		trxToolsBuilder.GetTransactionTool,

		commonToolsBuilder.GetUTCISOTimestampTool,
	} {
		toolItem, err := toolFn()
		if err != nil {
			slog.Error("error building tool " + toolItem.Name())
			return
		}

		agentTools = append(agentTools, toolItem)
	}

	transactionTrackerAgent, err := llmagent.New(llmagent.Config{
		Name:        "transaction_tracker_agent",
		Model:       model,
		Description: "Records, updates, and/or fetches daily transaction(s) of requesting users",
		Instruction: `
			You are a helpful transaction tracker assistant that helps users record, update, fetch, and/or summarize transaction details.

			If an account doesn't exist for the user's email, create an account for them before executing any of their requests.
			If an account already exists, don't try to create an account. It means you can immediately proceed with the transaction operations.
			Return the email under which the user has been enrolled so the user knows the identifier for his/her transactions tracking account.

			Only trust the get_current_utc_iso_timestamp_tool tool for fetching the current UTC ISO timestamps.

			Limit the transaction operations to the scope under the user's own email. 
			Do not allow a user to modify other users' transactions.

			Do not allow empty transaction Date input, if you cannot conclude the transaction date to be set from the user's prompt, 
			please set it as current UTC ISO using the get_current_utc_iso_timestamp_tool tool.

			After creating/updating a transaction, be sure to fetch the updated transaction record, using the get_transaction_tool or 
			list_transaction_tool (whichever is more fitting).

			When returning a transaction detail(s), be sure to return all the fields eventhough it's obvious.
			In your responses, be fun and add emojis so your responses look helpful.
		`,
		Tools: agentTools,
	})
	if err != nil {
		slog.Error("error building agent")
		return
	}

	sessionSvc := session.InMemoryService()

	adkRunner, e := runner.New(runner.Config{
		AppName:           appName,
		Agent:             transactionTrackerAgent,
		SessionService:    sessionSvc,
		AutoCreateSession: true,
	})
	if e != nil {
		slog.Error("error build runner")
		return
	}

	agentRunner := &agentrunner.EphemeralRunner{
		AppName:        appName,
		Runner:         adkRunner,
		SessionService: sessionSvc,
	}

	queueWorker := applications.QueueWorker{
		QueueManager: queueMgr,
		ChatManager:  slackMgr,
		AgentRunner:  agentRunner,
	}

	wg.Add(1)
	go queueWorker.Run(ctx, &wg)
	go httpServer.Run(ctx, &wg)

	slog.Info("Initialized App!")

	wg.Wait()

	slog.Info("Exiting App!")
}

func main() {
	app()
}
