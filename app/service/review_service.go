package service

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

// ReviewService memakai DUA database sekaligus: data kelas dan KRS dibaca
// dari PostgreSQL, ulasan disimpan di MongoDB. Service-lah satu-satunya
// tempat yang mengenal keduanya.
type ReviewService struct {
	reviews repository.ReviewRepository
	courses repository.CourseRepository
	perms   *helper.PermissionSet
}

func NewReviewService(
	reviews repository.ReviewRepository,
	courses repository.CourseRepository,
	perms *helper.PermissionSet,
) *ReviewService {
	return &ReviewService{reviews: reviews, courses: courses, perms: perms}
}

// ---------- POST /courses/:id/reviews ----------
// Create godoc
// @Summary Membuat ulasan kelas
// @Description Membuat ulasan untuk kelas yang diikuti oleh mahasiswa yang sedang login.
// @Tags Reviews
// @Accept json
// @Produce json
// @Param id path int true "ID kelas"
// @Param request body model.CreateReviewRequest true "Data ulasan"
// @Success 201 {object} map[string]interface{} "Ulasan berhasil disimpan"
// @Failure 400 {object} map[string]interface{} "ID kelas atau body tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Mahasiswa bukan peserta kelas"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Mahasiswa sudah pernah memberikan ulasan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id}/reviews [post]
func (s *ReviewService) Create(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id kelas harus berupa angka positif")
	}

	var req model.CreateReviewRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	req.Comment = strings.TrimSpace(req.Comment)
	req.Tags = NormalizeTags(req.Tags)
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	// Tidak ada FOREIGN KEY yang menghubungkan MongoDB dengan PostgreSQL.
	// Pemeriksaan "kelasnya ada" dan "mahasiswa ini peserta kelasnya" harus
	// dilakukan sendiri oleh service, dengan bertanya ke PostgreSQL.
	if _, err := s.courses.FindByID(ctx, courseID); err != nil {
		return translateReviewError(err)
	}
	enrolled, err := s.courses.IsEnrolled(ctx, courseID, current.UserID)
	if err != nil {
		return helper.Internal(err)
	}
	if !enrolled {
		return helper.NewError(fiber.StatusForbidden, "NOT_ENROLLED",
			"hanya peserta kelas yang boleh menulis ulasan")
	}

	created, err := s.reviews.Create(ctx, model.Review{
		CourseID: courseID, UserID: current.UserID, Username: current.Username,
		Rating: req.Rating, Comment: req.Comment, Tags: req.Tags,
	})
	if err != nil {
		return translateReviewError(err)
	}
	return helper.Created(c, "ulasan berhasil disimpan", created,
		"/api/v1/reviews/"+created.ID)
}

// ---------- GET /courses/:id/reviews ----------
// ListByCourse godoc
// @Summary Mendapatkan daftar ulasan kelas
// @Description Mengambil daftar ulasan berdasarkan ID kelas dengan dukungan filter tag dan pagination.
// @Tags Reviews
// @Produce json
// @Param id path int true "ID kelas"
// @Param tag query string false "Filter berdasarkan tag ulasan"
// @Param before query string false "ID ulasan sebagai penanda pagination"
// @Param limit query int false "Jumlah ulasan per halaman (1-50)" default(10)
// @Success 200 {object} map[string]interface{} "Daftar ulasan berhasil diambil"
// @Failure 400 {object} map[string]interface{} "ID kelas tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id}/reviews [get]
func (s *ReviewService) ListByCourse(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id kelas harus berupa angka positif")
	}

	q := model.ReviewQuery{
		CourseID: courseID,
		Tag:      strings.ToLower(strings.TrimSpace(c.Query("tag"))),
		Before:   c.Query("before"),
		Limit:    c.QueryInt("limit", 10),
	}
	if q.Limit < 1 || q.Limit > 50 {
		q.Limit = 10
	}

	// Minta satu lebih banyak untuk mengetahui apakah masih ada halaman
	// berikutnya: teknik yang sama dengan Modul 7.
	want := q.Limit
	q.Limit = want + 1

	reviews, err := s.reviews.FindByCourse(ctx, q)
	if err != nil {
		return translateReviewError(err)
	}

	hasMore := len(reviews) > want
	if hasMore {
		reviews = reviews[:want]
	}
	next := ""
	if hasMore {
		next = reviews[len(reviews)-1].ID
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "daftar ulasan berhasil diambil",
		"data":    reviews,
		"meta":    fiber.Map{"limit": want, "has_more": hasMore, "next_before": next},
	})
}

// ---------- GET /courses/:id/reviews/summary ----------
// Summary godoc
// @Summary Mendapatkan ringkasan ulasan kelas
// @Description Mengambil ringkasan ulasan berdasarkan ID kelas, termasuk informasi rating.
// @Tags Reviews
// @Produce json
// @Param id path int true "ID kelas"
// @Success 200 {object} map[string]interface{} "Ringkasan ulasan berhasil diambil"
// @Failure 400 {object} map[string]interface{} "ID kelas tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Kelas tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /courses/{id}/reviews/summary [get]
func (s *ReviewService) Summary(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id kelas harus berupa angka positif")
	}
	if _, err := s.courses.FindByID(ctx, courseID); err != nil {
		return translateReviewError(err)
	}

	summary, err := s.reviews.Summary(ctx, courseID)
	if err != nil {
		return helper.Internal(err)
	}
	return helper.Success(c, fiber.StatusOK, "ringkasan ulasan berhasil diambil", summary)
}

// ---------- GET /reviews/:id ----------
// Get godoc
// @Summary Mendapatkan detail ulasan
// @Description Mengambil detail ulasan berdasarkan ID ulasan.
// @Tags Reviews
// @Produce json
// @Param id path string true "ID ulasan"
// @Success 200 {object} map[string]interface{} "Ulasan berhasil ditemukan"
// @Failure 400 {object} map[string]interface{} "ID ulasan tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Ulasan tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /reviews/{id} [get]
func (s *ReviewService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	review, err := s.reviews.FindByID(ctx, c.Params("id"))
	if err != nil {
		return translateReviewError(err)
	}
	return helper.Success(c, fiber.StatusOK, "ulasan ditemukan", review)
}

// ---------- PATCH /reviews/:id ----------
// Update godoc
// @Summary Memperbarui ulasan
// @Description Memperbarui rating, komentar, atau tag pada ulasan milik mahasiswa yang sedang login.
// @Tags Reviews
// @Accept json
// @Produce json
// @Param id path string true "ID ulasan"
// @Param request body model.UpdateReviewRequest true "Data ulasan yang akan diperbarui"
// @Success 200 {object} map[string]interface{} "Ulasan berhasil diperbarui"
// @Failure 400 {object} map[string]interface{} "ID atau body tidak valid, atau tidak ada field yang diubah"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Bukan pemilik ulasan"
// @Failure 404 {object} map[string]interface{} "Ulasan tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Konflik pembaruan ulasan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /reviews/{id} [patch]
func (s *ReviewService) Update(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	var req model.UpdateReviewRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if req.Rating == nil && req.Comment == nil && req.Tags == nil {
		return helper.BadRequest("tidak ada field yang diubah")
	}

	if req.Comment != nil {
		trimmed := strings.TrimSpace(*req.Comment)
		req.Comment = &trimmed
	}
	if req.Tags != nil {
		normalized := NormalizeTags(*req.Tags)
		req.Tags = &normalized
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	updated, err := s.reviews.UpdateOwn(ctx, c.Params("id"), current.UserID, req)
	if err != nil {
		return translateReviewError(err)
	}
	return helper.Success(c, fiber.StatusOK, "ulasan berhasil diubah", updated)
}

// ---------- DELETE /reviews/:id ----------
// Pemilik boleh menghapus ulasannya sendiri; pemegang permission
// review:moderate boleh menghapus ulasan siapa pun.
// Delete godoc
// @Summary Menghapus ulasan
// @Description Menghapus ulasan milik sendiri atau ulasan pengguna lain jika memiliki permission review:moderate.
// @Tags Reviews
// @Produce json
// @Param id path string true "ID ulasan"
// @Success 204 "Ulasan berhasil dihapus"
// @Failure 400 {object} map[string]interface{} "ID ulasan tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki izin menghapus ulasan"
// @Failure 404 {object} map[string]interface{} "Ulasan tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /reviews/{id} [delete]
func (s *ReviewService) Delete(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	anyOwner := s.perms.Can(current.Role, "review:moderate")
	if err := s.reviews.Delete(ctx, c.Params("id"), current.UserID, anyOwner); err != nil {
		return translateReviewError(err)
	}
	return helper.NoContent(c)
}

// ---------- POST /reviews/:id/replies ----------
// Reply godoc
// @Summary Menambahkan balasan ulasan
// @Description Menambahkan balasan pada ulasan berdasarkan ID ulasan.
// @Tags Reviews
// @Accept json
// @Produce json
// @Param id path string true "ID ulasan"
// @Param request body model.ReplyRequest true "Isi balasan"
// @Success 201 {object} map[string]interface{} "Balasan berhasil ditambahkan"
// @Failure 400 {object} map[string]interface{} "ID ulasan atau body tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Ulasan tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Batas jumlah balasan tercapai"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /reviews/{id}/replies [post]
func (s *ReviewService) Reply(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	var req model.ReplyRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	req.Body = strings.TrimSpace(req.Body)
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	updated, err := s.reviews.AddReply(ctx, c.Params("id"), model.Reply{
		UserID: current.UserID, Username: current.Username, Body: req.Body,
	})
	if err != nil {
		return translateReviewError(err)
	}
	return helper.Success(c, fiber.StatusCreated, "balasan berhasil ditambahkan", updated)
}

// ---------- POST /reviews/:id/helpful ----------
// MarkHelpful godoc
// @Summary Memberikan penilaian helpful pada ulasan
// @Description Memberikan penilaian bahwa ulasan bermanfaat. Setiap pengguna hanya dapat memberikan satu penilaian pada ulasan yang sama.
// @Tags Reviews
// @Produce json
// @Param id path string true "ID ulasan"
// @Success 200 {object} map[string]interface{} "Penilaian berhasil diberikan"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 404 {object} map[string]interface{} "Ulasan tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Pengguna sudah memberikan penilaian atau mencoba menilai ulasannya sendiri"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /reviews/{id}/helpful [post]
func (s *ReviewService) MarkHelpful(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	updated, err := s.reviews.MarkHelpful(ctx, c.Params("id"), current.UserID)
	if err != nil {
		return translateReviewError(err)
	}
	return helper.Success(c, fiber.StatusOK, "terima kasih atas penilaiannya", updated)
}

func translateReviewError(err error) error {
	switch {
	case errors.Is(err, repository.ErrInvalidID):
		return helper.BadRequest("id ulasan tidak valid")
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound("data tidak ditemukan")
	case errors.Is(err, repository.ErrDuplicate):
		return helper.NewError(fiber.StatusConflict, "ALREADY_REVIEWED",
			"Anda sudah menulis ulasan untuk kelas ini, ubah ulasan yang ada")
	case errors.Is(err, repository.ErrNotOwner):
		return helper.Forbidden("hanya pemilik ulasan yang boleh melakukan ini")
	case errors.Is(err, repository.ErrOwnReview):
		return helper.NewError(fiber.StatusConflict, "OWN_REVIEW",
			"tidak dapat menilai ulasan sendiri")
	case errors.Is(err, repository.ErrAlreadyVoted):
		return helper.NewError(fiber.StatusConflict, "ALREADY_VOTED",
			"Anda sudah menilai ulasan ini")
	case errors.Is(err, repository.ErrReplyLimit):
		return helper.NewError(fiber.StatusConflict, "REPLY_LIMIT",
			"jumlah balasan sudah mencapai batas")
	default:
		return helper.Internal(err)
	}
}