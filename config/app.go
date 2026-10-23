package config

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
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

	// Penampung terakhir untuk URL yang tidak dikenal. Ikut melewati
	// ErrorHandler yang sama dengan kegagalan lainnya.
	app.Use(func(c *fiber.Ctx) error {
		return helper.NotFound("endpoint tidak ditemukan")
	})

	return app
}

// newErrorHandler adalah SATU-SATUNYA tempat error berubah menjadi
// response HTTP di seluruh aplikasi.
func newErrorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		requestID := helper.RequestID(c)

		var appErr *helper.AppError
		var fiberErr *fiber.Error

		switch {
		case errors.As(err, &appErr):
			// Kegagalan yang sudah kita rencanakan.

		case errors.As(err, &fiberErr) &&
			fiberErr.Code == fiber.StatusRequestEntityTooLarge:
			appErr = &helper.AppError{
				Status:  fiber.StatusRequestEntityTooLarge,
				Code:    "PAYLOAD_TOO_LARGE",
				Message: "ukuran body melebihi batas yang diizinkan",
			}

		case errors.As(err, &fiberErr):
			// Kegagalan bawaan Fiber, misalnya 405 atau 404 dari router.
			appErr = &helper.AppError{
				Status: fiberErr.Code, Code: "HTTP_ERROR",
				Message: fiberErr.Message,
			}

		default:
			// Kegagalan yang tidak kita duga.
			appErr = helper.Internal(err)
		}

		// Tingkat log ditentukan oleh SIAPA yang salah, bukan seberapa
		// mengganggu kegagalannya. 4xx adalah kesalahan pemakai API (WARN);
		// hanya 5xx yang berarti sistem kita rusak (ERROR). Bila tertukar,
		// pemantauan berteriak setiap ada yang salah ketik URL dan diam
		// ketika database benar-benar mati.
		if appErr.Status >= fiber.StatusInternalServerError {
			logger.Error("request_failed",
				slog.String("request_id", requestID),
				slog.String("path", c.Path()),
				slog.String("code", appErr.Code),
				slog.Int("status", appErr.Status),
				slog.String("error", err.Error()))
		} else {
			logger.Warn("request_rejected",
				slog.String("request_id", requestID),
				slog.String("path", c.Path()),
				slog.String("code", appErr.Code),
				slog.Int("status", appErr.Status))
		}

		return c.Status(appErr.Status).JSON(model.ErrorResponse{
			Success:   false,
			Code:      appErr.Code,
			Message:   appErr.Message,
			Fields:    appErr.Fields,
			RequestID: requestID,
		})
	}
}