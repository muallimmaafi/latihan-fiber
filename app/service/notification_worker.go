
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

// NotificationWorker mengerjakan task notification:send.
type NotificationWorker struct {
	users         repository.UserRepository
	notifications repository.NotificationRepository
	tx            *repository.TxManager
	mailer        helper.Mailer
	logger        *slog.Logger
}

func NewNotificationWorker(
	users repository.UserRepository,
	notifications repository.NotificationRepository,
	tx *repository.TxManager,
	mailer helper.Mailer,
	logger *slog.Logger,
) *NotificationWorker {
	return &NotificationWorker{
		users:         users,
		notifications: notifications,
		tx:            tx,
		mailer:        mailer,
		logger:        logger,
	}
}

// HandleSend memproses notifikasi dengan mekanisme idempotency.
func (w *NotificationWorker) HandleSend(
	ctx context.Context,
	t *asynq.Task,
) error {
	var job model.NotificationJob

	if err := json.Unmarshal(t.Payload(), &job); err != nil {
		return fmt.Errorf("%w: payload task rusak: %v", asynq.SkipRetry, err)
	}

	var event model.EnrollmentEvent

	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return fmt.Errorf(
			"%w: payload event %d rusak: %v",
			asynq.SkipRetry,
			job.OutboxID,
			err,
		)
	}

	user, err := w.users.FindByID(ctx, event.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf(
			"%w: user %d tidak ditemukan",
			asynq.SkipRetry,
			event.UserID,
		)
	}
	if err != nil {
		return err
	}

	msg, err := BuildMessage(job.EventType, event, user)
	if err != nil {
		return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
	}

	alreadySent := false

	err = w.tx.WithTxOnce(ctx, func(tx repository.DBTX) error {
		claimed, err := w.notifications.WithTx(tx).Claim(
			ctx,
			model.Notification{
				OutboxID: job.OutboxID,
				UserID:   user.ID,
				Channel:  "email",
				Subject:  msg.Subject,
				Body:     msg.Body,
			},
		)
		if err != nil {
			return err
		}

		if !claimed {
			alreadySent = true
			return nil
		}

		return w.mailer.Send(ctx, msg)
	})

	if errors.Is(err, helper.ErrPermanentDelivery) {
		return fmt.Errorf("%v: %w", err, asynq.SkipRetry)
	}
	if err != nil {
		return err
	}

	retry, _ := asynq.GetRetryCount(ctx)

	w.logger.Info(
		"notification_processed",
		slog.Int64("outbox_id", job.OutboxID),
		slog.String("event", job.EventType),
		slog.Int("retry", retry),
		slog.Bool("already_sent", alreadySent),
	)

	return nil
}
