package controllers

import (
	"bytes"
	"egy-go-adk-app/services/queue"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	adkchatsdk "github.com/fatihthedeveloper/adk-chat-sdk"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

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

		if c.Request().Header.Get("x-slack-signature") == "" {
			err = s.validateUsingBypassToken(c)
		} else {
			err = s.validateUsingSlackSignatureRawBody(c, rawBody)
		}
		if err != nil {
			slog.ErrorContext(c.Request().Context(), "untrusted source: "+err.Error())
			return c.String(http.StatusBadRequest, "Untrusted Source!")
		}

		// 4. Process the event (e.g., push to SQS)
		slog.Debug("received slack event",
			"type", envelope.Event.Type,
			"event_id", envelope.EventID,
		)

		messageId, sessionId := envelope.Event.TS, envelope.Event.TS
		threadHistory := "[]"
		if envelope.Event.ThreadTS != "" {
			sessionId = envelope.Event.ThreadTS

			threadHistoryRawData, _ := s.ChatManager.FetchThread(adkchatsdk.FetchThreadRequest{
				ChannelId: envelope.Event.Channel,
				ThreadId:  sessionId,
			})

			jsonThreadHistory, _ := json.Marshal(threadHistoryRawData)
			threadHistory = string(jsonThreadHistory)
		}

		type structuredCommand struct {
			UserEmail     string
			Prompt        string
			ThreadHistory string
		}

		newEmail := fmt.Sprintf("%s@kresnofatihimani.slack.com", envelope.Event.User)

		cmd, _ := json.Marshal(structuredCommand{
			UserEmail:     newEmail,
			Prompt:        envelope.Event.Text,
			ThreadHistory: threadHistory,
		})

		slog.InfoContext(c.Request().Context(), "prompt: "+string(cmd))

		s.QueueManager.Publish(c.Request().Context(), queue.Tasks, queue.QueueMessage{
			Id: uuid.NewString(),
			Body: map[string]string{
				"user":      envelope.Event.User,
				"sessionId": sessionId,
				"command":   string(cmd),
				"messageId": messageId,
				"channelId": envelope.Event.Channel,
			},
		})

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
	bypassToken := s.BypassToken
	requestToken := c.Request().Header.Get("x-slack-bypass-token")
	if requestToken != bypassToken {
		return errors.New("Invalid Token")
	}
	return nil
}
