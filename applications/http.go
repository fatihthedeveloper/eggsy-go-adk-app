package applications

import (
	"context"
	"egy-go-adk-app/controllers"
	"log/slog"
	"net/http"
	"sync"

	"github.com/labstack/echo/v5"
)

type HttpServer struct {
	Controllers []controllers.Controller
}

func (h *HttpServer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	e := echo.New()

	for _, controller := range h.Controllers {
		e.Add(controller.GetMethod(), controller.GetPath(), controller.GetHandler())
	}

	slog.Info("HTTP server starting on :7000")

	// StartConfig.Start accepts a context directly.
	// When ctx is cancelled (e.g. SIGINT/SIGTERM), it drains in-flight
	// requests and shuts down cleanly — no separate Shutdown() call needed.
	sc := echo.StartConfig{Address: ":7000"}
	if err := sc.Start(ctx, e); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server error", "err", err)
	}

	slog.Info("HTTP server shut down")
}
