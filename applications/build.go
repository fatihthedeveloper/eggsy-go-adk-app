package applications

import (
	"context"
	commonTools "egy-go-adk-app/agents/common/tools"
	trxTools "egy-go-adk-app/agents/transactiontracker/tools"
	"egy-go-adk-app/services/agentrunner"
	"log"
	"log/slog"
	"net/http"
	"os"
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

const AppName = "transactions_tracker_app"

// MustEnv returns the value of the environment variable or aborts if it is unset.
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

// DefaultHTTPClient is the shared outbound HTTP client (Slack, Cloudflare, Gemini).
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// NewSlackManager builds the Slack chat manager used to react/reply to messages.
func NewSlackManager(httpClient *http.Client) *adkchatsdk.SlackChatManager {
	return &adkchatsdk.SlackChatManager{
		SdkConfig: adkchatsdk.SlackSdkConfig{
			SigningSecret: MustEnv("SLACK_SIGNING_SECRET"),
			APIToken:      MustEnv("SLACK_API_TOKEN"),
		},
		Client: httpClient,
	}
}

// BuildAgentRunner wires the model, tools, agent, and ADK runner into an ephemeral
// runner. This used to live inline in main.go's app(). It is expensive, so the Lambda
// worker calls it ONCE per cold start and reuses the result for warm invocations.
func BuildAgentRunner(ctx context.Context, httpClient *http.Client) (*agentrunner.EphemeralRunner, error) {
	model, err := gemini.NewModel(ctx, "gemini-flash-latest", &genai.ClientConfig{
		APIKey: MustEnv("GEMINI_API_KEY"),
	})
	if err != nil {
		return nil, err
	}

	cloudflareConfig := &commoncloudflared1sdk.D1Config{
		ProjectId:  MustEnv("CLOUDFLARE_ACCOUNT_ID"),
		DatabaseId: MustEnv("CLOUDFLARE_D1_DATABASE_ID"),
		APIToken:   MustEnv("CLOUDFLARE_D1_API_TOKEN"),
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
			return nil, err
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
		return nil, err
	}

	sessionSvc := session.InMemoryService()
	adkRunner, err := runner.New(runner.Config{
		AppName:           AppName,
		Agent:             transactionTrackerAgent,
		SessionService:    sessionSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, err
	}

	slog.Info("agent runner built")
	return &agentrunner.EphemeralRunner{
		AppName:        AppName,
		Runner:         adkRunner,
		SessionService: sessionSvc,
	}, nil
}
