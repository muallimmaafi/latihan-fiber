package service

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/hibiken/asynq"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

type QueueInspector interface {
	GetQueueInfo(queue string) (*asynq.QueueInfo, error)
}

type NotificationService struct {
	notifications repository.NotificationRepository
	outbox        repository.OutboxRepository
	inspector     QueueInspector
}

func NewNotificationService(
	notifications repository.NotificationRepository,
	outbox repository.OutboxRepository,
	inspector QueueInspector,
) *NotificationService {
	return &NotificationService{
		notifications: notifications,
		outbox:        outbox,
		inspector:     inspector,
	}
}

// ---------- GET /notifications/mine ----------
// Mine godoc
// @Summary Mendapatkan notifikasi pengguna
// @Description Mengambil maksimal 20 notifikasi milik pengguna yang sedang login.
// @Tags Notifications
// @Produce json
// @Success 200 {object} map[string]interface{} "Notifikasi berhasil diambil"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /notifications/mine [get]
func (s *NotificationService) Mine(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	list, err := s.notifications.FindByUser(ctx, current.UserID, 20)
	if err != nil {
		return helper.Internal(err)
	}

	return helper.Success(c, fiber.StatusOK, "notifikasi berhasil diambil", list)
}

// ---------- GET /admin/queue ----------
// Monitor godoc
// @Summary Memantau status queue dan notifikasi
// @Description Mengambil statistik outbox, jumlah notifikasi terkirim, serta status antrean Redis dan Asynq.
// @Tags Admin
// @Produce json
// @Success 200 {object} map[string]interface{} "Statistik queue berhasil diambil"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki permission queue:monitor"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /admin/queue [get]
func (s *NotificationService) Monitor(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	pending, published, oldest, err := s.outbox.Stats(ctx)
	if err != nil {
		return helper.Internal(err)
	}

	sent, err := s.notifications.Count(ctx)
	if err != nil {
		return helper.Internal(err)
	}

	stats := model.QueueStats{
		OutboxPending:     pending,
		OutboxPublished:   published,
		OldestPendingAge:  oldest.Round(time.Millisecond).String(),
		NotificationsSent: sent,
	}

	// Redis yang mati tidak boleh membuat endpoint ini gagal seluruhnya.
	info, err := s.inspector.GetQueueInfo(model.QueueNotifications)
	if err != nil {
		stats.QueueError = "queue tidak dapat dibaca: Redis tidak dapat dihubungi atau queue belum pernah dipakai"
	} else {
		stats.Queue = &model.QueueInfo{
			Name:      info.Queue,
			Pending:   info.Pending,
			Active:    info.Active,
			Scheduled: info.Scheduled,
			Retry:     info.Retry,
			Archived:  info.Archived,
			Completed: info.Completed,
			Processed: info.Processed,
			Failed:    info.Failed,
		}
	}

	return helper.Success(c, fiber.StatusOK, "keadaan queue berhasil diambil", stats)
}
