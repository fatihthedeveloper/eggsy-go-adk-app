# Migration Plan: Lightsail Container → AWS Lambda (Python Receiver + Go Worker)

## 1. Why this change

The service today is a **single long-running process** (`main.go`) that runs two
goroutines side-by-side:

- An **HTTP server** ([applications/http.go](applications/http.go)) on `:7000` that
  receives Slack webhooks ([controllers/slackwebhook.go](controllers/slackwebhook.go)),
  pushes a job onto an in-memory queue, and returns `200 OK`.
- A **background worker** ([applications/worker.go](applications/worker.go)) that polls
  that queue every 5s and runs the Gemini/ADK agent
  ([services/agentrunner/runner.go](services/agentrunner/runner.go)), which can take
  many seconds.

This decoupling exists *only* because Slack requires the webhook to be acknowledged in
**under 3 seconds** (the practical target is ~2s; otherwise Slack retries and the user
sees errors). On Lightsail the container is always running, so the in-memory queue +
goroutine works fine — but you pay 24/7 for an idle container.

On Lambda there is no persistent process, so an in-memory queue and a polling goroutine
cannot survive between invocations. We replace the *process-level* decoupling with an
*invocation-level* one, split across two functions:

```
                          async invoke (InvocationType=Event)
  Slack ─HTTP─▶ [Receiver Lambda]  ──────────────────────────▶ [Worker Lambda]
               Python, ~30 lines                                Go (this codebase)
               - verify Slack signature                         - parse Slack event
               - answer url_verification                        - build email / command
               - boto3 async-invoke worker                      - fetch thread history
               - return 200 in <2s                              - build & run ADK agent
               NO business logic                                - reply to Slack
                                                                runs up to minutes
```

- **Receiver Lambda (Python)** = a dumb, language-agnostic transport shim. Its only job
  is: verify the Slack signature → answer the handshake → forward the **raw Slack event
  verbatim** to the worker via async invoke → return `200`. It contains **no business
  logic** — no email construction, no command shaping, no Slack history fetch.
- **Worker Lambda (Go)** = the old `QueueWorker.Process` + all the controller's
  request-parsing logic + the whole agent build from `main.go`. Bigger memory, long
  timeout. Not on Slack's latency critical path.

### Why the receiver is Python, not Go

The receiver needs nothing from this Go codebase — it only knows about Slack signatures
and `lambda:InvokeFunction`. Keeping it as a tiny Python zip means:

- **All Go business logic stays in Go**, in one place (the worker). The only contract
  between the two functions is the **raw Slack event JSON** — a stable, Slack-defined
  shape, not something we maintain.
- **No duplicated logic across languages.** The receiver never parses the event meaning;
  it forwards bytes.
- **Smallest possible cold start** — `boto3` is already in the Lambda Python runtime, so
  there are zero dependencies to package and no container image to pull.
- The "one image vs two images for two Lambdas" question disappears entirely: the
  receiver isn't a Go artifact at all.

---

## 2. Trigger mechanism decision

**Primary (matches the plan): direct async Lambda invoke from the receiver.**
The Python receiver calls `boto3` `lambda.invoke(InvocationType="Event")`. AWS queues and
runs the worker; the call returns to the receiver in single-digit milliseconds. No extra
infrastructure.

**Alternative to keep in mind: SQS between the two Lambdas.**
The receiver writes to SQS; the worker is triggered by the SQS event source. This buys
automatic **retries, a dead-letter queue, batching, and backpressure**. With this design
the swap is trivial — the receiver changes one boto3 call (`sqs.send_message` instead of
`lambda.invoke`) and the worker's event-source binding changes; the worker's Go logic is
untouched because it would just parse the same forwarded payload out of the SQS record.

> Recommendation: ship the **direct async invoke** first (simplest, exactly the stated
> design). Async invoke already retries twice on failure and supports an
> [on-failure destination / DLQ](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html),
> which is usually enough. Move to SQS only if you need stronger delivery guarantees.

---

## 3. Code changes overview

| Area | Today | After |
|---|---|---|
| Receiver | `echo` HTTP server + `SlackWebhookController` | **Python** Lambda (zip), Function URL trigger |
| Receiver→worker handoff | `QueueManager.Publish` to in-memory queue | `boto3` async `lambda.invoke` (in Python) |
| Worker entry | worker goroutine + `QueueWorker.Run` poll loop | **Go** Lambda invoked per-event; loop deleted |
| Request parsing (email, command, sessionId) | in `SlackWebhookController` | **moved into the Go worker handler** |
| Thread-history fetch | synchronous in the webhook handler | **moved into the worker** (off the <2s path) |
| Job processing | `QueueWorker.Process` | reused **almost verbatim** in the worker |
| Agent build | inside `app()` in `main.go` | moved to a shared builder, called once per cold start |
| Deploy | one Docker image on Lightsail | Python zip (receiver) + Go zip/image (worker) |

The Go that gets reused as-is or with minor edits: `applications/worker.go`'s `Process`,
the agent runner, all `agents/*` tools, the slack manager, the Cloudflare/account SDK
wiring. The genuinely new code is **one Python file** and **one thin Go worker `main`**,
plus extracting the agent build into a shared builder.

---

## 4. Detailed changes

### 4.1 Add Lambda dependency (Go worker only)

The worker just needs the Lambda Go runtime. No AWS SDK is required in Go anymore — the
invoke happens in Python.

```bash
go get github.com/aws/aws-lambda-go/lambda
```

### 4.2 Extract a shared agent/dependency builder

Right now `app()` in [main.go](main.go) wires up the model, tools, agent, ADK runner,
ephemeral runner, slack manager, and queue. Pull the reusable parts into a builder so the
worker's cold-start path can call it once.

**New file: `applications/build.go`**

```go
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

func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

// NewSlackManager is used by the worker to react/reply.
func NewSlackManager(httpClient *http.Client) *adkchatsdk.SlackChatManager {
	return &adkchatsdk.SlackChatManager{
		SdkConfig: adkchatsdk.SlackSdkConfig{
			SigningSecret: MustEnv("SLACK_SIGNING_SECRET"),
			APIToken:      MustEnv("SLACK_API_TOKEN"),
		},
		Client: httpClient,
	}
}

// BuildAgentRunner contains everything that used to live inline in main.go's app().
// The worker calls this ONCE per cold start (see 4.4) and reuses it for warm invokes.
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

	transactionSvc, _ := eggsytransactionsdk.CloudFlareD1TransactionServiceBuilder{
		HttpClient: httpClient, CloudFlareConfig: cloudflareConfig,
	}.Build()
	accountSvc, _ := eggsyaccountsdk.CloudFlareD1AccountServiceBuilder{
		HttpClient: httpClient, CloudFlareConfig: cloudflareConfig,
	}.Build()

	trxToolsBuilder := trxTools.NativeImplTransactionTrackerToolsBuilder{
		TrxService: transactionSvc, AccService: accountSvc,
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
		Instruction: `...keep the exact instruction string currently in main.go...`,
		Tools:       agentTools,
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

func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
```

> Note: `session.InMemoryService()` is per-cold-start. That's fine — the
> `EphemeralRunner` already deletes the session after each run
> ([services/agentrunner/runner.go:21](services/agentrunner/runner.go#L21)), and each
> Slack thread is self-contained (thread history is passed in the prompt, see 4.5).

### 4.3 Receiver Lambda (Python)

A single file. It verifies the Slack signature, answers the `url_verification`
handshake, and forwards the **raw request body verbatim** to the worker. It deliberately
does **not** parse the event meaning — no email, no command, no history fetch.

**New file: `receiver/handler.py`**

```python
import base64
import hashlib
import hmac
import json
import os
import time

import boto3

_lambda = boto3.client("lambda")
SIGNING_SECRET = os.environ["SLACK_SIGNING_SECRET"].encode()
WORKER_FUNCTION_NAME = os.environ["WORKER_FUNCTION_NAME"]


def _verify_signature(headers: dict, raw_body: str) -> bool:
    ts = headers.get("x-slack-request-timestamp", "")
    sig = headers.get("x-slack-signature", "")
    if not ts or not sig:
        return False
    # Replay guard: reject anything older than 5 minutes.
    if abs(time.time() - int(ts)) > 60 * 5:
        return False
    basestring = f"v0:{ts}:{raw_body}".encode()
    expected = "v0=" + hmac.new(SIGNING_SECRET, basestring, hashlib.sha256).hexdigest()
    return hmac.compare_digest(expected, sig)


def handler(event, _context):
    raw_body = event.get("body") or ""
    if event.get("isBase64Encoded"):
        raw_body = base64.b64decode(raw_body).decode()

    parsed = json.loads(raw_body)

    # Slack URL verification handshake — must echo the challenge.
    if parsed.get("type") == "url_verification":
        return {"statusCode": 200, "body": parsed.get("challenge", "")}

    # Function URL lower-cases header names, but normalize defensively.
    headers = {k.lower(): v for k, v in (event.get("headers") or {}).items()}
    if not _verify_signature(headers, raw_body):
        return {"statusCode": 401, "body": "Untrusted Source!"}

    # Forward the raw Slack event verbatim. The Go worker does ALL parsing.
    _lambda.invoke(
        FunctionName=WORKER_FUNCTION_NAME,
        InvocationType="Event",  # async, fire-and-forget
        Payload=raw_body.encode(),
    )

    # Ack Slack immediately. The worker sets the first emoji reaction.
    return {"statusCode": 200, "body": ""}
```

> The payload sent to the worker is the **exact bytes Slack sent**. The contract between
> the two functions is therefore Slack's own `SlackEventEnvelope` shape — nothing custom.

### 4.4 Worker Lambda entry point (Go)

The worker now receives the **raw Slack event envelope** (forwarded by the receiver) and
does the request-parsing that used to live in `SlackWebhookController`: derive
`messageId` / `sessionId`, construct the email, build the `command` JSON, then run the
existing `Process`. The agent runner is built **once** at cold start in `init()` so warm
invocations skip the expensive model/tool wiring.

**New file: `cmd/worker/main.go`**

```go
package main

import (
	"context"
	"egy-go-adk-app/applications"
	"egy-go-adk-app/services/queue"
	"egy-go-adk-app/utilities"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-lambda-go/lambda"
	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"github.com/google/uuid"
)

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
		// No QueueManager: the worker is event-driven, not polled.
	}
}

// The event IS the raw Slack envelope forwarded verbatim by the Python receiver.
func handle(ctx context.Context, envelope adkchatsdk.SlackEventEnvelope) error {
	// --- request parsing (formerly in SlackWebhookController) ---
	messageId, sessionId := envelope.Event.TS, envelope.Event.TS
	if envelope.Event.ThreadTS != "" {
		sessionId = envelope.Event.ThreadTS
	}

	newEmail := fmt.Sprintf("%s@kresnofatihimani.slack.com", envelope.Event.User)
	cmd, _ := json.Marshal(map[string]string{
		"UserEmail": newEmail,
		"Prompt":    envelope.Event.Text,
		// ThreadHistory is filled in by Process (see 4.5).
	})

	msg := queue.QueueMessage{
		Id: uuid.NewString(),
		Body: map[string]string{
			"user":      envelope.Event.User,
			"sessionId": sessionId,
			"command":   string(cmd),
			"messageId": messageId,
			"channelId": envelope.Event.Channel,
		},
	}

	worker.Process(ctx, msg)
	return nil
}

func main() {
	lambda.Start(handle)
}
```

**Edits to [applications/worker.go](applications/worker.go):**

- **Delete** `Run(ctx, wg)` and its `select`/`time.After(5s)` poll loop — the Lambda
  event source replaces it.
- **Delete** the `QueueManager` field (and the `Ack` call inside the loop). Keep
  `AgentRunner` and `ChatManager`.
- **Keep `Process` essentially as-is.** It already does: set loading emoji → run agent →
  unset emoji → reply. One addition: do the **thread-history fetch here** (see 4.5).

### 4.5 Move thread-history fetch into the worker

Today the controller calls `s.ChatManager.FetchThread(...)` *synchronously* before
returning ([controllers/slackwebhook.go:75](controllers/slackwebhook.go#L75)) — a
blocking Slack round-trip on the critical path. The receiver no longer does this at all.
Move it into `QueueWorker.Process`:

- In `Process`, when `sessionId != messageId` (i.e. a threaded message), call
  `FetchThread`, marshal it, and merge it into the `command` JSON (`ThreadHistory` field)
  before invoking the agent.

### 4.6 Signature validation lives in Python (4.3)

A Function URL is publicly reachable, so the signature **must** be verified. The current
Go `validateUsingBypassToken`
([controllers/slackwebhook.go:134](controllers/slackwebhook.go#L134)) only checks the
header is *non-empty* — it does not actually verify anything. The Python `_verify_signature`
in 4.3 does the real HMAC-SHA256 check against the raw body + timestamp, with a replay
guard. This is now the single enforcement point; the Go side no longer validates.

### 4.7 Delete / retire

- `applications/http.go` (echo server) — no longer used. Keep only for a local-dev mode.
- `controllers/slackwebhook.go` — its parsing logic moves into the worker handler (4.4)
  and its validation into Python (4.6). Retire it (or keep behind a local-dev build tag).
- `QueueWorker.Run` poll loop and the `QueueManager` field (4.4).
- The `go queueWorker.Run(...)` / `go httpServer.Run(...)` / `wg.Wait()` orchestration in
  [main.go](main.go). Keep `main.go` as a local-dev entry point behind a build tag, or
  remove it.
- In-memory and Cloudflare queue managers are unused at runtime but harmless to keep.

---

## 5. Build & packaging

The "same image for both Lambdas" question is moot — the two functions are different
languages and ship as different artifacts.

### Receiver (Python, zip — no Docker)

```bash
cd receiver
zip ../receiver.zip handler.py     # boto3 is already in the runtime; nothing to vendor
```

Runtime `python3.13`, handler `handler.handler`.

### Worker (Go)

Zip deploy with the `provided.al2023` custom runtime (smaller + faster cold start than a
container image):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o bootstrap ./cmd/worker
zip worker.zip bootstrap
```

Runtime `provided.al2023`, handler `bootstrap`. (If you prefer a container image, use the
`public.ecr.aws/lambda/provided:al2023` base — but zip is simpler here.)

---

## 6. Infrastructure / config (Terraform or SAM)

| Resource | Receiver (Python) | Worker (Go) |
|---|---|---|
| Runtime | `python3.13` | `provided.al2023` |
| Trigger | **Lambda Function URL** (or API Gateway) | invoked async by receiver |
| Memory | 128 MB | 512 MB–1 GB (model + tool init) |
| Timeout | 5–10 s | 60–300 s (agent runs) |
| Concurrency | default | consider a reserved cap to bound Gemini/D1 load |
| Env vars | `SLACK_SIGNING_SECRET`, `WORKER_FUNCTION_NAME` | `SLACK_SIGNING_SECRET`, `SLACK_API_TOKEN`, `GEMINI_API_KEY`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_D1_DATABASE_ID`, `CLOUDFLARE_D1_API_TOKEN` |

**IAM:** the receiver's execution role needs `lambda:InvokeFunction` on the worker's ARN.
Both need the basic `AWSLambdaBasicExecutionRole` (CloudWatch Logs).

**Slack:** repoint the Event Subscription Request URL to the receiver's Function URL.
Slack will re-do the `url_verification` handshake (handled in 4.3).

---

## 7. Latency & cold-start notes

- **Receiver** must answer Slack in < ~2–3 s. It does exactly two things before
  returning: one HMAC computation (microseconds) and one async `lambda.invoke` (a few
  ms). Python cold starts for a no-dependency function are ~100–250 ms — comfortably
  within budget. It makes **zero** Slack API calls, so nothing external blocks the ack.
- **Worker** cold start pays for `BuildAgentRunner` (model client + tool wiring). That's
  fine — it's async and off Slack's clock. If worker cold starts ever feel slow to the
  end user, add **provisioned concurrency = 1** on the worker.

---

## 8. Security note (do this regardless of the migration)

[env.ignore.json](env.ignore.json) and
[docker-run.ignore.ps1](docker-run.ignore.ps1) contain **live secrets** (Slack tokens,
Gemini key, Cloudflare D1 tokens). In the Lambda model, store these in **SSM Parameter
Store (SecureString)** or **Secrets Manager** and inject via env vars / fetch on cold
start — do not bake them into the artifact. Given they're committed to the repo, **rotate
all of them** as part of this work.

---

## 9. Suggested rollout order

1. Add the Go dep (4.1) and `applications/build.go` (4.2); confirm `go build ./...` passes.
2. Add `cmd/worker` (4.4) and the `Process` thread-fetch (4.5); deploy the worker; test by
   invoking it manually with a sample **raw Slack event** JSON.
3. Add `receiver/handler.py` (4.3); deploy behind a Function URL with
   `WORKER_FUNCTION_NAME` set; grant `lambda:InvokeFunction`.
4. Point a **test** Slack app at the receiver URL; verify the handshake, the < 2s ack, and
   end-to-end flow.
5. Cut the production Slack app over; decommission the Lightsail container service.
6. Delete retired Go code (4.7).

---

## 10. Files at a glance

**New**
- `receiver/handler.py` — Python transport shim (verify → forward raw event → 200)
- `applications/build.go` — shared agent/slack/http builder
- `cmd/worker/main.go` — Go worker: parses the Slack event, runs the agent, replies

**Modified**
- `applications/worker.go` — drop `Run`/poll loop + `QueueManager` field; `Process` gains
  the thread-history fetch

**Reused unchanged**
- `agents/**`, `services/agentrunner/runner.go`, the slack manager, the Cloudflare/account
  SDK wiring, `utilities/logging.go`

**Retired (Lambda no longer uses them; keep only for local dev)**
- `applications/http.go`, `controllers/slackwebhook.go`, the `echo` server in `main.go`,
  the in-memory / Cloudflare queue managers
