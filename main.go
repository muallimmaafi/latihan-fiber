package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"latihan-fiber/app/repository"
	"latihan-fiber/config"
	"latihan-fiber/database"
)

var metodeBerbody = map[string]bool{
	fiber.MethodPost:  true,
	fiber.MethodPut:   true,
	fiber.MethodPatch: true,
}

// requireJSON menolak request berisi body yang Content-Type-nya bukan JSON.
// Status yang tepat untuk kasus ini adalah 415, bukan 400.
func requireJSON(c *fiber.Ctx) error {
	if metodeBerbody[c.Method()] {
		ct := c.Get("Content-Type")
		if !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
			return fail(c, fiber.StatusUnsupportedMediaType,
				"Content-Type harus application/json")
		}
	}
	return c.Next()
}

func main() {
	// 1. Konfigurasi
	config.LoadEnv()

	// 2. Koneksi basis data
	pool, err := database.NewPool(context.Background())
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	// 3. Perakitan: pool -> repository -> handler
	userRepository := repository.NewUserRepository(pool)
	userHandler := NewUserHandler(userRepository)

	// 4. Aplikasi
	app := fiber.New(fiber.Config{
		AppName: "Praktikum Backend Lanjut - Pertemuan 3",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			pesan := "terjadi kesalahan pada server"
			if e, ok := err.(*fiber.Error); ok {
				status = e.Code
				pesan = e.Message
			}
			return fail(c, status, pesan)
		},
	})

	app.Use(requestid.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${locals:requestid} ${method} ${path} ${status} ${latency}\n",
	}))
	app.Use(cors.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	api := app.Group("/api/v1")

	api.Get("/health", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()

		// Kesehatan layanan kini ikut bergantung pada basis data.
		if err := pool.Ping(ctx); err != nil {
			return fail(c, fiber.StatusServiceUnavailable,
				"database tidak dapat dihubungi")
		}
		return ok(c, "server dan database berjalan", nil)
	})

	u := api.Group("/users", requireJSON)
	u.Get("/", userHandler.List)
	u.Get("/:id", userHandler.Get)
	u.Post("/", userHandler.Create)
	u.Put("/:id", userHandler.Replace)
	u.Patch("/:id", userHandler.Patch)
	u.Delete("/:id", userHandler.Delete)

	// Endpoint yang tidak dikenal
	app.Use(func(c *fiber.Ctx) error {
		return fail(c, fiber.StatusNotFound, "endpoint tidak ditemukan")
	})

	port := config.GetEnv("APP_PORT", "3000")
	log.Printf("Server berjalan di http://localhost:%s", port)
	log.Fatal(app.Listen(":" + port))
}