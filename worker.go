
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/app/service"
	"latihan-fiber/config"
	"latihan-fiber/helper"
)

// startWorker menyalakan Outbox Relay dan Asynq Server.
func startWorker(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
) func() {
	mailer, err := helper.NewLogMailer(
		"logs/mail.log",
		time.Duration(config.GetEnvInt("MAILER_DELAY_MS", 1500))*time.Millisecond,
		config.GetEnvInt("MAILER_FAIL_EVERY", 0),
	)
	if err != nil {
		logger.Error(
			"gagal menyiapkan mailer",
			slog.String("error", err.Error()),
		)
		return nil
	}

	outboxRepository := repository.NewOutboxRepository(pool)
	txManager := repository.NewTxManager(pool)

	client := asynq.NewClient(config.RedisOpt())

	relay := service.NewOutboxRelay(
		outboxRepository,
		txManager,
		client,
		logger,
		time.Duration(config.GetEnvInt("RELAY_INTERVAL_MS", 1000))*time.Millisecond,
		config.GetEnvInt("RELAY_BATCH", 100),
	)

	worker := service.NewNotificationWorker(
		repository.NewUserRepository(pool),
		repository.NewNotificationRepository(pool),
		txManager,
		mailer,
		logger,
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(model.TaskSendNotification, worker.HandleSend)

	srv := config.NewWorkerServer(logger)

	if err := srv.Start(mux); err != nil {
		logger.Error(
			"gagal menyalakan worker",
			slog.String("error", err.Error()),
		)
		_ = client.Close()
		return nil
	}

	relayDone := make(chan struct{})

	go func() {
		relay.Run(ctx)
		close(relayDone)
	}()

	logger.Info("worker berjalan")

	return func() {
		<-relayDone
		srv.Shutdown()
		_ = client.Close()
	}
}
