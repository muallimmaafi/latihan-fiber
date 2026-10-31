package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/hibiken/asynq"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
)

type TaskEnqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

type OutboxRelay struct {
	outbox   repository.OutboxRepository
	tx       *repository.TxManager
	queue    TaskEnqueuer
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

func NewOutboxRelay(
	outbox repository.OutboxRepository,
	tx *repository.TxManager,
	queue TaskEnqueuer,
	logger *slog.Logger,
	interval time.Duration,
	batch int,
) *OutboxRelay {
	if interval <= 0 {
		interval = time.Second
	}
	if batch <= 0 {
		batch = 100
	}

	return &OutboxRelay{
		outbox:   outbox,
		tx:       tx,
		queue:    queue,
		logger:   logger,
		interval: interval,
		batch:    batch,
	}
}

func (r *OutboxRelay) RelayOnce(ctx context.Context) error {
	var published int
	var enqueueErr error

	err := r.tx.WithTxOnce(ctx, func(tx repository.DBTX) error {
		repo := r.outbox.WithTx(tx)

		events, err := repo.FetchUnpublished(ctx, r.batch)
		if err != nil {
			return err
		}

		ids := make([]int64, 0, len(events))

		for _, event := range events {
			if err := r.publish(ctx, event); err != nil {
				enqueueErr = err
				break
			}
			ids = append(ids, event.ID)
		}

		if len(ids) > 0 {
			if err := repo.MarkPublished(ctx, ids); err != nil {
				return err
			}
		}

		published = len(ids)
		return nil
	})

	if err != nil {
		return err
	}
	if enqueueErr != nil {
		return enqueueErr
	}

	r.logger.Info("relay_done", slog.Int("published", published))
	return nil
}

func (r *OutboxRelay) publish(ctx context.Context, event model.OutboxEvent) error {
	payload, err := json.Marshal(model.NotificationJob{
		OutboxID:  event.ID,
		EventType: event.EventType,
		Payload:   json.RawMessage(event.Payload),
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(
		model.TaskSendNotification,
		payload,
		asynq.TaskID("outbox-"+jsonID(event.ID)),
		asynq.Queue(model.QueueNotifications),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
		asynq.Retention(24*time.Hour),
	)

	_, err = r.queue.EnqueueContext(ctx, task)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

func jsonID(id int64) string {
	return json.Number(stringInt(id)).String()
}

func stringInt(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (r *OutboxRelay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		if err := r.RelayOnce(ctx); err != nil {
			r.logger.Warn("relay_delayed", slog.String("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
