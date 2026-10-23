package config

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/helper"
	"latihan-fiber/middleware"
	"latihan-fiber/route"
)

// NewApp merakit aplikasi: membuat instance Fiber, memasang middleware,
// lalu mendaftarkan route.
func NewApp(logger *slog.Logger, deps route.Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      GetEnv("APP_NAME", "Praktikum Backend Lanjut"),
		ErrorHandler: newErrorHandler(logger),
		// Membatasi ukuran body mencegah satu request besar menghabiskan
		// memori server (denial of service yang paling murah dilakukan).
		BodyLimit: 1 * 1024 * 1024, // 1 MB
	})

	middleware.Register(app, logger, GetEnv("ALLOWED_ORIGINS", ""))
	route.Register(app, deps)

	// Penampung terakhir untuk URL yang tidak dikenal.
	app.Use(func(c *fiber.Ctx) error {
		return helper.Fail(c, fiber.StatusNotFound, "endpoint tidak ditemukan")
	})

	return app
}

// newErrorHandler adalah jaring pengaman terakhir: error yang tidak
// tertangani di service berakhir di sini dengan format yang tetap konsisten.
func newErrorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		status := fiber.StatusInternalServerError
		message := "terjadi error pada server"

		if e, ok := err.(*fiber.Error); ok {
			status = e.Code
			message = e.Message
		}

		logger.Error("unhandled_error",
			slog.String("path", c.Path()),
			slog.Int("status", status),
			slog.String("error", err.Error()),
		)
		return helper.Fail(c, status, message)
	}
}