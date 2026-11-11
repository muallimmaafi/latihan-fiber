package service

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

// Kesalahan khusus layer service: aturan bisnis yang tidak diketahui
// repository.
var errCreditLimit = errors.New("batas SKS terlampaui")

type CourseService struct {
	courses repository.CourseRepository
	outbox  repository.OutboxRepository
	tx      *repository.TxManager
}

func NewCourseService(
	courses repository.CourseRepository,
	outbox repository.OutboxRepository,
	tx *repository.TxManager,
) *CourseService {
	return &CourseService{
		courses: courses,
		outbox:  outbox,
		tx:      tx,
	}
}

// ---------- GET /courses ----------
// List godoc
// @Summary Mendapatkan daftar kelas
// @Description Mengambil seluruh daftar kelas yang tersedia.
// @Tags Courses
// @Produce json
// @Success 200 {object} map[string]interface{} "Daftar kelas berhasil diambil"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/ [get]
func (s *CourseService) List(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	courses, err := s.courses.FindAll(ctx)
	if err != nil {
		return helper.Internal(err)
	}
	return helper.Success(c, fiber.StatusOK, "daftar kelas berhasil diambil", courses)
}

// ---------- GET /courses/mine ----------
// Mine godoc
// @Summary Mendapatkan daftar KRS mahasiswa
// @Description Mengambil daftar kelas yang diikuti mahasiswa yang sedang login, termasuk total SKS dan batas maksimal SKS.
// @Tags Courses
// @Produce json
// @Success 200 {object} map[string]interface{} "Daftar KRS berhasil diambil"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/mine [get]
func (s *CourseService) Mine(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	courses, err := s.courses.FindByStudent(ctx, current.UserID)
	if err != nil {
		return helper.Internal(err)
	}

	total := 0
	for _, course := range courses {
		total += course.Credits
	}
	return helper.Success(c, fiber.StatusOK, "KRS berhasil diambil", fiber.Map{
		"courses":       courses,
		"total_credits": total,
		"max_credits":   MaxCreditsPerSemester,
	})
}

// ---------- GET /courses/:id ----------
// Get godoc
// @Summary Mendapatkan detail kelas
// @Description Mengambil detail kelas berdasarkan ID kelas.
// @Tags Courses
// @Produce json
// @Param id path int true "ID kelas"
// @Success 200 {object} map[string]interface{} "Detail kelas berhasil diambil"
// @Failure 400 {object} map[string]interface{} "ID kelas tidak valid"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id} [get]
func (s *CourseService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	course, err := s.courses.FindByID(ctx, id)
	if err != nil {
		return translateCourseError(err)
	}
	return helper.Success(c, fiber.StatusOK, "kelas ditemukan", course)
}

// ---------- PATCH /courses/:id ----------
// UpdateQuota godoc
// @Summary Memperbarui kuota kelas
// @Description Mengubah kuota kelas berdasarkan ID. Kuota baru tidak boleh lebih kecil dari jumlah peserta yang sudah terdaftar.
// @Tags Courses
// @Accept json
// @Produce json
// @Param id path int true "ID kelas"
// @Param request body map[string]int true "Kuota baru, contoh: {\"quota\": 40}"
// @Success 200 {object} map[string]interface{} "Kuota kelas berhasil diperbarui"
// @Failure 400 {object} map[string]interface{} "ID atau kuota tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki permission course:update"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id} [patch]
func (s *CourseService) UpdateQuota(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	var req struct {
		Quota int `json:"quota"`
	}

	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("format request tidak valid")
	}

	if req.Quota <= 0 {
		return helper.BadRequest("kuota harus lebih dari 0")
	}

	var updated model.Course

	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		repo := s.courses.WithTx(tx)

		current, err := repo.FindByID(ctx, id)
		if err != nil {
			return err
		}

		if req.Quota < current.EnrolledCount {
			return errors.New("kuota tidak boleh lebih kecil dari jumlah peserta")
		}

		updated, err = repo.UpdateQuota(ctx, id, req.Quota)
		if err != nil {
			return err
		}

		return s.outbox.WithTx(tx).Insert(
			ctx,
			model.EventCourseQuotaChanged,
			model.CourseQuotaChangedEvent{
				CourseID:   updated.ID,
				CourseCode: updated.Code,
				CourseName: updated.Name,
				NewQuota:   updated.Quota,
			},
		)
	})

	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return helper.NotFound("kelas tidak ditemukan")
		}
		return helper.Internal(err)
	}

	return helper.Success(
		c,
		fiber.StatusOK,
		"kuota kelas berhasil diperbarui",
		updated,
	)
}

// ---------- POST /courses/:id/enroll ----------
// Enroll godoc
// @Summary Mendaftar ke kelas
// @Description Mendaftarkan mahasiswa yang sedang login ke kelas berdasarkan ID. Sistem memeriksa kuota kelas dan batas maksimal SKS.
// @Tags Courses
// @Produce json
// @Param id path int true "ID kelas"
// @Success 201 {object} map[string]interface{} "Berhasil mendaftar kelas"
// @Failure 400 {object} map[string]interface{} "ID kelas tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Kuota penuh, sudah terdaftar, atau batas maksimal SKS terlampaui"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id}/enroll [post]
func (s *CourseService) Enroll(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	var enrolled model.Course
	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		repo := s.courses.WithTx(tx)

		if err := repo.LockStudent(ctx, current.UserID); err != nil {
			return err
		}

		course, err := repo.FindByID(ctx, courseID)
		if err != nil {
			return err
		}

		taken, err := repo.SumCredits(ctx, current.UserID)
		if err != nil {
			return err
		}

		if !WithinCreditLimit(taken, course.Credits) {
			return errCreditLimit
		}

		enrolled, err = repo.IncrementEnrolled(ctx, courseID)
		if err != nil {
			return err
		}

		// Catat event dalam transaction yang sama.
		if err := s.outbox.WithTx(tx).Insert(
			ctx,
			model.EventEnrollmentCreated,
			model.EnrollmentEvent{
				UserID:     current.UserID,
				CourseID:   enrolled.ID,
				CourseCode: enrolled.Code,
				CourseName: enrolled.Name,
			},
		); err != nil {
			return err
		}

		return repo.InsertEnrollment(ctx, courseID, current.UserID)
	})
	if err != nil {
		return translateCourseError(err)
	}

	return helper.Created(c, "berhasil mendaftar kelas", enrolled,
		"/api/v1/courses/"+strconv.Itoa(courseID))
}

// ---------- DELETE /courses/:id/enroll ----------
// Drop godoc
// @Summary Membatalkan pendaftaran kelas
// @Description Menghapus pendaftaran kelas mahasiswa yang sedang login dan memperbarui jumlah peserta.
// @Tags Courses
// @Produce json
// @Param id path int true "ID kelas"
// @Success 204 "Pendaftaran kelas berhasil dibatalkan"
// @Failure 400 {object} map[string]interface{} "ID kelas tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Mahasiswa tidak terdaftar di kelas"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id}/enroll [delete]
func (s *CourseService) Drop(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		repo := s.courses.WithTx(tx)

		if err := repo.LockStudent(ctx, current.UserID); err != nil {
			return err
		}

		if err := repo.DeleteEnrollment(ctx, courseID, current.UserID); err != nil {
			return err
		}

		course, err := repo.DecrementEnrolled(ctx, courseID)
		if err != nil {
			return err
		}

		// Event yang benar untuk penghapusan KRS adalah enrollment.dropped.
		// Event disimpan dalam transaction yang sama agar ikut rollback
		// jika proses penghapusan KRS gagal.
		return s.outbox.WithTx(tx).Insert(
			ctx,
			model.EventEnrollmentDropped,
			model.EnrollmentEvent{
				UserID:     current.UserID,
				CourseID:   course.ID,
				CourseCode: course.Code,
				CourseName: course.Name,
			},
		)
	})
	if err != nil {
		return translateCourseError(err)
	}

	return helper.NoContent(c)
}

// ---------- POST /courses/swap ----------
func (s *CourseService) Swap(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	var req struct {
		FromCourseID int `json:"from_course_id"`
		ToCourseID   int `json:"to_course_id"`
	}

	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("format request tidak valid")
	}

	if req.FromCourseID <= 0 || req.ToCourseID <= 0 {
		return helper.BadRequest("id kelas harus berupa angka positif")
	}

	if req.FromCourseID == req.ToCourseID {
		return helper.BadRequest("kelas asal dan tujuan tidak boleh sama")
	}

	var joined model.Course

	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		repo := s.courses.WithTx(tx)

		// 1. Kunci KRS mahasiswa.
		if err := repo.LockStudent(ctx, current.UserID); err != nil {
			return err
		}

		// 2. Ambil data kelas asal dan tujuan.
		from, err := repo.FindByID(ctx, req.FromCourseID)
		if err != nil {
			return err
		}

		to, err := repo.FindByID(ctx, req.ToCourseID)
		if err != nil {
			return err
		}

		// 3. Pastikan mahasiswa terdaftar di kelas asal.
		enrolled, err := repo.IsEnrolled(ctx, req.FromCourseID, current.UserID)
		if err != nil {
			return err
		}
		if !enrolled {
			return repository.ErrNotEnrolled
		}

		// 4. Periksa batas SKS setelah perpindahan.
		taken, err := repo.SumCredits(ctx, current.UserID)
		if err != nil {
			return err
		}

		if !WithinCreditLimit(taken-from.Credits, to.Credits) {
			return errCreditLimit
		}

		// 5. Kurangi peserta kelas asal.
		if _, err := repo.DecrementEnrolled(ctx, req.FromCourseID); err != nil {
			return err
		}

		// 6. Hapus pendaftaran kelas asal.
		if err := repo.DeleteEnrollment(ctx, req.FromCourseID, current.UserID); err != nil {
			return err
		}

		// 7. Ambil kursi kelas tujuan secara atomik.
		joined, err = repo.IncrementEnrolled(ctx, req.ToCourseID)
		if err != nil {
			return err
		}

		// 8. Simpan pendaftaran kelas tujuan.
		if err := repo.InsertEnrollment(ctx, req.ToCourseID, current.UserID); err != nil {
			return err
		}

		// 9. Catat satu event perpindahan kelas ke outbox.
		return s.outbox.WithTx(tx).Insert(
			ctx,
			model.EventEnrollmentSwapped,
			model.EnrollmentEvent{
				UserID:         current.UserID,
				CourseID:       to.ID,
				CourseCode:     to.Code,
				CourseName:     to.Name,
				FromCourseCode: from.Code,
				FromCourseName: from.Name,
			},
		)
	})

	if err != nil {
		return translateCourseError(err)
	}

	return helper.Success(
		c,
		fiber.StatusOK,
		"berhasil berpindah kelas",
		joined,
	)
}

// translateCourseError memetakan kegagalan ke response.
func translateCourseError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound("kelas tidak ditemukan")
	case errors.Is(err, repository.ErrCourseFull):
		return helper.NewError(fiber.StatusConflict, "COURSE_FULL",
			"kuota kelas sudah penuh")
	case errors.Is(err, repository.ErrDuplicate):
		return helper.NewError(fiber.StatusConflict, "ALREADY_ENROLLED",
			"Anda sudah terdaftar di kelas ini")
	case errors.Is(err, repository.ErrNotEnrolled):
		return helper.NewError(fiber.StatusConflict, "NOT_ENROLLED",
			"Anda tidak terdaftar di kelas tersebut")
	case errors.Is(err, errCreditLimit):
		return helper.NewError(fiber.StatusConflict, "CREDIT_LIMIT_EXCEEDED",
			"batas "+strconv.Itoa(MaxCreditsPerSemester)+" SKS terlampaui")
	default:
		return helper.Internal(err)
	}
}
