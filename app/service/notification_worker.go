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
	courses       repository.CourseRepository
	notifications repository.NotificationRepository
	tx            *repository.TxManager
	mailer        helper.Mailer
	logger        *slog.Logger
}

func NewNotificationWorker(
	users repository.UserRepository,
	courses repository.CourseRepository,
	notifications repository.NotificationRepository,
	tx *repository.TxManager,
	mailer helper.Mailer,
	logger *slog.Logger,
) *NotificationWorker {
	return &NotificationWorker{
		users:         users,
		courses:       courses,
		notifications: notifications,
		tx:            tx,
		mailer:        mailer,
		logger:        logger,
	}
}

// HandleSend memproses task notifikasi berdasarkan jenis event.
func (w *NotificationWorker) HandleSend(
	ctx context.Context,
	t *asynq.Task,
) error {
	var job model.NotificationJob

	if err := json.Unmarshal(t.Payload(), &job); err != nil {
		return fmt.Errorf("%w: payload task rusak: %v", asynq.SkipRetry, err)
	}

	switch job.EventType {
	case model.EventCourseQuotaChanged:
		return w.handleQuotaChanged(ctx, job)

	case model.EventEnrollmentCreated,
		model.EventEnrollmentDropped,
		model.EventEnrollmentSwapped:
		return w.handleEnrollment(ctx, job)

	default:
		return fmt.Errorf(
			"%w: jenis event tidak dikenal: %s",
			asynq.SkipRetry,
			job.EventType,
		)
	}
}

// handleEnrollment memproses notifikasi untuk event enrollment.
func (w *NotificationWorker) handleEnrollment(
	ctx context.Context,
	job model.NotificationJob,
) error {
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

	return w.sendNotification(ctx, job, user, msg)
}

// handleQuotaChanged mengirim notifikasi kepada seluruh peserta kelas.
func (w *NotificationWorker) handleQuotaChanged(
	ctx context.Context,
	job model.NotificationJob,
) error {
	var event model.CourseQuotaChangedEvent

	if err := json.Unmarshal(job.Payload, &event); err != nil {
		return fmt.Errorf(
			"%w: payload perubahan kuota event %d rusak: %v",
			asynq.SkipRetry,
			job.OutboxID,
			err,
		)
	}

	participants, err := w.courses.FindParticipants(ctx, event.CourseID)
	if err != nil {
		return err
	}

	for _, user := range participants {
		if err := ctx.Err(); err != nil {
			return err
		}

		msg := BuildQuotaChangedMessage(event, user)

		if err := w.sendNotification(ctx, job, user, msg); err != nil {
			return fmt.Errorf(
				"mengirim notifikasi perubahan kuota kepada user %d: %w",
				user.ID,
				err,
			)
		}
	}

	retry, _ := asynq.GetRetryCount(ctx)

	w.logger.Info(
		"quota_notifications_processed",
		slog.Int64("outbox_id", job.OutboxID),
		slog.Int("course_id", event.CourseID),
		slog.Int("participants", len(participants)),
		slog.Int("retry", retry),
	)

	return nil
}

// sendNotification mengirim satu notifikasi dengan mekanisme idempotency.
// Setiap penerima diproses dalam transaksi terpisah agar notifikasi yang
// sudah berhasil tidak dikirim ulang ketika penerima berikutnya gagal.
func (w *NotificationWorker) sendNotification(
	ctx context.Context,
	job model.NotificationJob,
	user model.User,
	msg model.Message,
) error {
	alreadySent := false

	err := w.tx.WithTxOnce(ctx, func(tx repository.DBTX) error {
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
		slog.Int("user_id", user.ID),
		slog.Int("retry", retry),
		slog.Bool("already_sent", alreadySent),
	)

	return nil
}