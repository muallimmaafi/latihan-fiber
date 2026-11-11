
package service

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

// UserService memegang dua tanggung jawab sekaligus pada struktur baku
// mata kuliah ini: menerima *fiber.Ctx (peran controller) dan menjalankan
// business rules (peran use case).
type UserService struct {
	repo  repository.UserRepository
	perms *helper.PermissionSet
}

func NewUserService(
	repo repository.UserRepository,
	perms *helper.PermissionSet,
) *UserService {
	return &UserService{repo: repo, perms: perms}
}

// ---------- GET /users ----------
// Dijaga middleware dengan permission user:list.

// List godoc
// @Summary Mendapatkan daftar pengguna
// @Description Mengambil daftar pengguna dengan pencarian, filter status aktif, dan cursor pagination. Mendukung format JSON dan CSV.
// @Tags Users
// @Accept json
// @Produce json
// @Produce text/csv
// @Param limit query int false "Jumlah data per halaman (default 10, maksimal 100)"
// @Param search query string false "Kata kunci pencarian pengguna"
// @Param is_active query bool false "Filter status aktif pengguna"
// @Param cursor query string false "Cursor untuk mengambil halaman berikutnya"
// @Success 200 {object} map[string]interface{} "Daftar pengguna berhasil diambil"
// @Failure 400 {object} map[string]interface{} "Parameter query tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki permission user:list"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/ [get]
func (s *UserService) List(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	// Format dipilih SEBELUM query dijalankan. Bila client meminta format
	// yang tidak dapat kita hasilkan, tidak ada gunanya membebani database
	// untuk hasil yang akan dibuang.
	format, err := helper.Negotiate(c, helper.FormatJSON, helper.FormatCSV)
	if err != nil {
		return err
	}

	q, err := helper.ParseCursorQuery(c)
	if err != nil {
		return err
	}

	rows, err := s.repo.FindAfterCursor(ctx, q)
	if err != nil {
		return helper.Internal(err)
	}

	// Baris tambahan hasil limit+1 dipotong SEBELUM format dipilih, supaya
	// CSV dan JSON memuat jumlah baris yang sama.
	hasMore := len(rows) > q.Limit
	if hasMore {
		rows = rows[:q.Limit]
	}

	if format == helper.FormatCSV {
		return helper.WriteUsersCSV(c, rows)
	}

	meta := &model.CursorMeta{Limit: q.Limit, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		meta.NextCursor = helper.EncodeCursor(last.CreatedAt, last.ID)
	}
	return helper.SuccessCursor(c, "daftar user berhasil diambil", rows, meta)
}

// ---------- GET /users/:id ----------

// Get godoc
// @Summary Mendapatkan detail pengguna
// @Description Mengambil detail pengguna berdasarkan ID. Pengguna hanya dapat mengakses data sendiri kecuali memiliki permission user:read:any.
// @Tags Users
// @Produce json
// @Param id path int true "ID pengguna"
// @Success 200 {object} map[string]interface{} "Detail pengguna berhasil diambil"
// @Failure 400 {object} map[string]interface{} "ID pengguna tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki akses ke pengguna"
// @Failure 404 {object} map[string]interface{} "Pengguna tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/{id} [get]
func (s *UserService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	// Hak akses diperiksa SEBELUM data diambil, supaya keberadaan sebuah
	// id tidak dapat disimpulkan dari selisih waktu 403 dan 404.
	if !CanAccessUser(current, id, s.perms, "user:read:any") {
		return helper.Forbidden("tidak berhak mengakses data user lain")
	}

	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(err, "user")
	}
	return helper.Success(c, fiber.StatusOK, "user ditemukan", user)
}

// ---------- POST /users ----------
// Dijaga middleware dengan permission user:update:any.
// Create godoc
// @Summary Membuat pengguna baru
// @Description Membuat akun pengguna baru dengan role default user.
// @Tags Users
// @Accept json
// @Produce json
// @Param request body model.CreateUserRequest true "Data pengguna baru"
// @Success 201 {object} map[string]interface{} "Pengguna berhasil dibuat"
// @Failure 400 {object} map[string]interface{} "Body JSON tidak valid"
// @Failure 409 {object} map[string]interface{} "Username sudah digunakan"
// @Failure 422 {object} map[string]interface{} "Validasi data gagal"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/ [post]
func (s *UserService) Create(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	hashed, err := helper.HashPassword(req.Password)
	if err != nil {
		return helper.Internal(err)
	}

	newUser, err := s.repo.Create(ctx, model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hashed,
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		return translateError(err, "user")
	}

	return helper.Created(c, "user berhasil dibuat", newUser,
		"/api/v1/users/"+strconv.Itoa(newUser.ID))
}

// Replace godoc
// @Summary Mengganti seluruh data pengguna
// @Description Mengganti username, email, dan status aktif pengguna berdasarkan ID. Pengguna hanya dapat mengubah datanya sendiri kecuali memiliki permission user:update:any.
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "ID pengguna"
// @Param request body model.ReplaceUserRequest true "Data pengguna yang baru"
// @Success 200 {object} map[string]interface{} "Data pengguna berhasil diganti"
// @Failure 400 {object} map[string]interface{} "ID atau body JSON tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki akses"
// @Failure 404 {object} map[string]interface{} "Pengguna tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Username sudah digunakan"
// @Failure 422 {object} map[string]interface{} "Validasi data gagal"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/{id} [put]
// ---------- PUT /users/:id ----------
func (s *UserService) Replace(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}
	if !CanAccessUser(current, id, s.perms, "user:update:any") {
		return helper.Forbidden("tidak berhak mengubah data user lain")
	}

	var req model.ReplaceUserRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	result, err := s.repo.Update(ctx, model.User{
		ID:       id,
		Username: strings.TrimSpace(req.Username),
		Email:    strings.TrimSpace(req.Email),
		IsActive: req.IsActive,
	})
	if err != nil {
		return translateError(err, "user")
	}
	return helper.Success(c, fiber.StatusOK, "user berhasil diganti seluruhnya", result)
}

// Patch godoc
// @Summary Memperbarui sebagian data pengguna
// @Description Memperbarui satu atau beberapa field pengguna berdasarkan ID. Pengguna hanya dapat mengubah datanya sendiri kecuali memiliki permission user:update:any.
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "ID pengguna"
// @Param request body model.PatchUserRequest true "Field pengguna yang akan diperbarui"
// @Success 200 {object} map[string]interface{} "Data pengguna berhasil diperbarui"
// @Failure 400 {object} map[string]interface{} "ID atau body JSON tidak valid atau tidak ada field yang diubah"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki akses"
// @Failure 404 {object} map[string]interface{} "Pengguna tidak ditemukan"
// @Failure 409 {object} map[string]interface{} "Username sudah digunakan"
// @Failure 422 {object} map[string]interface{} "Validasi data gagal"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/{id} [patch]
// ---------- PATCH /users/:id ----------
func (s *UserService) Patch(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}
	if !CanAccessUser(current, id, s.perms, "user:update:any") {
		return helper.Forbidden("tidak berhak mengubah data user lain")
	}

	var req model.PatchUserRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	// Urutan penting: body kosong 400, field ada tetapi isinya buruk 422.
	if IsEmptyPatch(req) {
		return helper.BadRequest("tidak ada field yang diubah")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(err, "user")
	}

	result, err := s.repo.Update(ctx, ApplyPatch(existing, req))
	if err != nil {
		return translateError(err, "user")
	}
	return helper.Success(c, fiber.StatusOK, "user berhasil diperbarui sebagian", result)
}

// ---------- PATCH /users/:id/role ----------
// Dijaga middleware dengan permission role:assign.
// AssignRole godoc
// @Summary Mengubah role pengguna
// @Description Mengubah role pengguna berdasarkan ID. Memerlukan permission role:assign dan validasi hak akses.
// @Tags Users
// @Accept json
// @Produce json
// @Param id path int true "ID pengguna"
// @Param request body model.AssignRoleRequest true "Role baru pengguna"
// @Success 200 {object} map[string]interface{} "Role pengguna berhasil diubah"
// @Failure 400 {object} map[string]interface{} "ID atau body JSON tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki akses"
// @Failure 404 {object} map[string]interface{} "Pengguna tidak ditemukan"
// @Failure 422 {object} map[string]interface{} "Validasi role gagal"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/{id}/role [patch]
func (s *UserService) AssignRole(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	var req model.AssignRoleRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := ValidateAssignRole(current, id, req, s.perms); len(errs) > 0 {
		return helper.Validation(errs)
	}

	result, err := s.repo.UpdateRole(ctx, id, strings.TrimSpace(req.Role))
	if err != nil {
		return translateError(err, "user")
	}
	return helper.Success(c, fiber.StatusOK, "role user berhasil diubah", result)
}

// ---------- DELETE /users/:id ----------
// Dijaga middleware dengan permission user:delete.
// Delete godoc
// @Summary Menghapus pengguna
// @Description Menghapus pengguna berdasarkan ID. Pengguna tidak diperbolehkan menghapus akunnya sendiri.
// @Tags Users
// @Produce json
// @Param id path int true "ID pengguna"
// @Success 204 "Pengguna berhasil dihapus"
// @Failure 400 {object} map[string]interface{} "ID pengguna tidak valid"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi"
// @Failure 403 {object} map[string]interface{} "Tidak memiliki akses atau mencoba menghapus akun sendiri"
// @Failure 404 {object} map[string]interface{} "Pengguna tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /users/{id} [delete]
func (s *UserService) Delete(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	// Punya permission menghapus tidak berarti boleh menghapus dirinya sendiri.
	if current.UserID == id {
		return helper.Forbidden("tidak boleh menghapus akun sendiri")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return translateError(err, "user")
	}
	return helper.NoContent(c)
}

// translateError mengubah error milik repository menjadi AppError.
//
// Tanda tangannya tidak memuat fiber.Ctx: fungsi ini hanya menerjemahkan
// satu jenis error menjadi jenis lain, dan tidak tahu apa pun tentang HTTP.
// Yang tidak dikenali menjadi Internal (fail closed): lebih baik membalas
// 500 daripada menebak status.
func translateError(err error, entity string) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound(entity + " tidak ditemukan")
	case errors.Is(err, repository.ErrDuplicate):
		return helper.Conflict("username sudah dipakai")
	default:
		return helper.Internal(err)
	}
}
