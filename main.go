package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"latihan-fiber/app/repository"
	"latihan-fiber/app/service"
	"latihan-fiber/config"
	"latihan-fiber/database"
	"latihan-fiber/helper"
	"latihan-fiber/route"
)

const minSecretLength = 32

func main() {
	config.LoadEnv()
	logger := config.NewLogger()

	// Rahasia diperiksa SEBELUM server menyala. Lebih baik gagal seketika
	// daripada berjalan dengan token yang mudah dipalsukan.
	jwtSecret := config.GetEnv("JWT_SECRET", "")
	if len(jwtSecret) < minSecretLength {
		logger.Error("JWT_SECRET tidak diisi atau terlalu pendek",
			slog.Int("minimal_karakter", minSecretLength))
		os.Exit(1)
	}

	pool, err := database.NewPool(context.Background())
	if err != nil {
		logger.Error("gagal terhubung ke database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	mongoClient, err := database.NewMongoClient(context.Background())
	if err != nil {
		logger.Error("gagal terhubung ke MongoDB", slog.String("error", err.Error()))
		os.Exit(1)
	}
	// Disconnect menutup seluruh koneksi di pool milik driver. Diberi batas
	// waktu sendiri, karena context milik request sudah tidak berlaku saat
	// aplikasi berhenti.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = mongoClient.Disconnect(ctx)
	}()

	mongoDB := mongoClient.Database(config.GetEnv("MONGO_DATABASE", "latihan_fiber"))

	jwtManager := helper.NewJWTManager(
		jwtSecret,
		config.GetEnv("JWT_ISSUER", "praktikum-backend"),
		time.Duration(config.GetEnvInt("JWT_ACCESS_TTL_MINUTES", 15))*time.Minute,
	)

	userRepository := repository.NewUserRepository(pool)
	tokenRepository := repository.NewTokenRepository(pool)
	roleRepository := repository.NewRoleRepository(pool)
	courseRepository := repository.NewCourseRepository(pool)
	txManager := repository.NewTxManager(pool)

	reviewRepository := repository.NewReviewRepository(mongoDB)
	if err := reviewRepository.EnsureIndexes(context.Background()); err != nil {
		logger.Error("gagal membuat index MongoDB", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Pemetaan role ke permission dibaca SEKALI saat aplikasi menyala.
	// Konsekuensinya: perubahan hak akses di database baru berlaku setelah
	// aplikasi dijalankan ulang. Itu keputusan sadar, bukan kelalaian.
	rawPermissions, err := roleRepository.LoadPermissions(context.Background())
	if err != nil {
		logger.Error("gagal memuat permission", slog.String("error", err.Error()))
		os.Exit(1)
	}
	permissions := helper.NewPermissionSet(rawPermissions)
	logger.Info("permission dimuat", slog.Any("roles", permissions.KnownRoles()))

	userService := service.NewUserService(userRepository, permissions)
	authService := service.NewAuthService(
		userRepository, tokenRepository, jwtManager, permissions,
		time.Duration(config.GetEnvInt("JWT_REFRESH_TTL_DAYS", 7))*24*time.Hour,
	)
	courseService := service.NewCourseService(courseRepository, txManager)
	reviewService := service.NewReviewService(reviewRepository, courseRepository, permissions)

	app := config.NewApp(logger, route.Dependencies{
		Pool:          pool,
		Mongo:         mongoClient,
		JWT:           jwtManager,
		Permissions:   permissions,
		UserService:   userService,
		AuthService:   authService,
		CourseService: courseService,
		ReviewService: reviewService,
	})

	port := config.GetEnv("APP_PORT", "3000")
	go func() {
		if err := app.Listen(":" + port); err != nil {
			logger.Error("server berhenti", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()
	logger.Info("server berjalan", slog.String("port", port))

	// Graceful shutdown: tunggu Ctrl+C, lalu beri waktu request
	// yang sedang berjalan untuk selesai.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("sinyal berhenti diterima, menutup server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		logger.Error("gagal menutup server dengan rapi",
			slog.String("error", err.Error()))
	}
	logger.Info("server berhenti dengan rapi")
}