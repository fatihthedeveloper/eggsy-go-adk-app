package controllers

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

type HealthController struct{}

func (h *HealthController) GetMethod() string {
	return http.MethodGet
}

func (h *HealthController) GetPath() string {
	return "/health"
}

func (h *HealthController) GetHandler() echo.HandlerFunc {
	return func(c *echo.Context) error {
		return c.String(http.StatusOK, "healthy")
	}
}
