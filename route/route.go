package route

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"latihan-fiber/app/service"
	"latihan-fiber/helper"
	"latihan-fiber/middleware"
)

// Dependencies mengumpulkan semua yang dibutuhkan route, supaya
// penambahan service berikutnya tidak mengubah tanda tangan Register.
type Dependencies struct {
	Pool        *pgxpool.Pool
	JWT         *helper.JWTManager
	UserService *service.UserService
	AuthService *service.AuthService
}

func Register(app *fiber.App, deps Dependencies) {
	api := app.Group("/api/v1")

	// --- publik ---
	api.Get("/health", healthCheck(deps.Pool))

	// --- autentikasi ---
	auth := api.Group("/auth", middleware.RequireJSON)
	auth.Post("/register", deps.AuthService.Register)
	auth.Post("/login", middleware.LoginRateLimiter(), deps.AuthService.Login)
	auth.Post("/refresh", deps.AuthService.Refresh)
	auth.Post("/logout", deps.AuthService.Logout)
	auth.Get("/me", middleware.RequireAuth(deps.JWT), deps.AuthService.Me)

	// --- wajib membawa access token ---
	users := api.Group("/users",
		middleware.RequireJSON, middleware.RequireAuth(deps.JWT))
	users.Get("/", deps.UserService.List)
	users.Get("/:id", deps.UserService.Get)
	users.Post("/", deps.UserService.Create)
	users.Put("/:id", deps.UserService.Replace)
	users.Patch("/:id", deps.UserService.Patch)
	users.Delete("/:id", deps.UserService.Delete)
}

// healthCheck melaporkan kondisi layanan beserta databasenya.
func healthCheck(pool *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			return helper.Fail(c, fiber.StatusServiceUnavailable,
				"database tidak dapat dihubungi")
		}
		return helper.Success(c, fiber.StatusOK, "server dan database berjalan", nil)
	}
}