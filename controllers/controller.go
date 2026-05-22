package controllers

import "github.com/labstack/echo/v5"

type Controller interface {
	GetMethod() string
	GetPath() string
	GetHandler() echo.HandlerFunc
}
