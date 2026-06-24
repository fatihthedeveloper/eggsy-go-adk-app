package controllers

import (
	"bytes"
	"egy-go-adk-app/services/queue"
	"egy-go-adk-app/services/slackmsg"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"github.com/labstack/echo/v5"
)

// SlackWebhookController is the local-dev HTTP entry point. In production the Slack
// webhook is fronted by the (Python) receiver Lambda, which forwards the raw event to
// the worker; see LAMBDA_MIGRATION.md. Both paths share slackmsg.BuildCommandMessage.
type SlackWebhookController struct {
	QueueManager queue.QueueManager
	ChatManager  adkchatsdk.ChatManager
	BypassToken  string
}

func (s *SlackWebhookController) GetMethod() string {
	return http.MethodPost
}

func (s *SlackWebhookController) GetPath() string {
	return "/slack"
}

func (s *SlackWebhookController) GetHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		rawBody, err := io.ReadAll(c.Request().Body)
		if err != nil {
			slog.Error("failed to read request body", "error", err)
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "failed to read request body",
			})
		}
		c.Request().Body = io.NopCloser(bytes.NewBuffer(rawBody))

		// 1. Initialize the struct
		var envelope adkchatsdk.SlackEventEnvelope

		// 2. Bind the JSON body to the struct
		if err := c.Bind(&envelope); err != nil {
			slog.Error("failed to bind slack event", "error", err)
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid request body",
			})
		}

		// 3. Handle the Slack URL Verification (if this is the first handshake)
		if envelope.Type == "url_verification" {
			return c.String(http.StatusOK, envelope.Challenge)
		}

		err = s.validateUsingBypassToken(c)
		if err != nil {
			slog.ErrorContext(c.Request().Context(), "untrusted source: "+err.Error())
			return c.String(http.StatusBadRequest, "Untrusted Source!")
		}

		// 4. Process the event (push to the queue for the worker to pick up)
		slog.Debug("received slack event",
			"type", envelope.Event.Type,
			"event_id", envelope.EventID,
		)

		msg := slackmsg.BuildCommandMessage(s.ChatManager, envelope)

		slog.InfoContext(c.Request().Context(), "prompt: "+msg.Body["command"])

		s.QueueManager.Publish(c.Request().Context(), queue.Tasks, msg)

		reactionReqBuilder := adkchatsdk.SlackReactToMessageRequestBuilder{
			ChannelId:        msg.Body["channelId"],
			ThreadTimestamp:  msg.Body["sessionId"],
			MessageTimestamp: msg.Body["messageId"],
			Emoji:            "tennis",
		}
		s.ChatManager.ReactToMessage(reactionReqBuilder.Build())

		// Always return 200 OK immediately to Slack
		return c.NoContent(http.StatusOK)
	}
}

func (s *SlackWebhookController) validateUsingSlackSignatureRawBody(c *echo.Context, rawBody []byte) error {
	signature, msgTs := c.Request().Header.Get("x-slack-signature"), c.Request().Header.Get("x-slack-request-timestamp")
	validateReqBuilder := adkchatsdk.SlackValidateRequestBuilder{
		Timestamp: msgTs,
		RawBody:   string(rawBody),
		Signature: signature,
	}
	return s.ChatManager.Validate(validateReqBuilder.Build())
}

func (s *SlackWebhookController) validateUsingBypassToken(c *echo.Context) error {
	slackSignature := c.Request().Header.Get("x-slack-signature")
	slackTimestamp := c.Request().Header.Get("x-slack-request-timestamp")
	if slackSignature == "" || slackTimestamp == "" {
		return fmt.Errorf("SlackSignature or SlackTimestamp was invalid!")
	}
	return nil
}
