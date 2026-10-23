package main

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
)

type UserHandler struct {
	repo repository.UserRepository
}

// Perhatikan tipe parameternya: INTERFACE, bukan struct konkret.
// Handler tidak tahu dan tidak perlu tahu datanya disimpan di mana.
func NewUserHandler(repo repository.UserRepository) *UserHandler {
	return &UserHandler{repo: repo}
}

func paramID(c *fiber.Ctx) (int, bool) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

// terjemahkanError memetakan error milik repository menjadi status HTTP.
// Satu tempat untuk seluruh handler, agar pemetaannya tidak tercecer.
func terjemahkanError(c *fiber.Ctx, err error, pesanUmum string) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return fail(c, fiber.StatusNotFound, "user tidak ditemukan")
	case errors.Is(err, repository.ErrDuplicate):
		return fail(c, fiber.StatusConflict, "username sudah dipakai")
	default:
		return fail(c, fiber.StatusInternalServerError, pesanUmum)
	}
}

func (h *UserHandler) List(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	q := parseListQuery(c)
	users, total, err := h.repo.FindAll(ctx, q)
	if err != nil {
		return fail(c, fiber.StatusInternalServerError, "gagal mengambil data user")
	}

	totalPages := 0
	if q.Limit > 0 {
		totalPages = (total + q.Limit - 1) / q.Limit
	}
	return okList(c, "daftar user berhasil diambil", users, &model.Meta{
		Page: q.Page, Limit: q.Limit, Total: total, TotalPages: totalPages,
	})
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	id, valid := paramID(c)
	if !valid {
		return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}
	user, err := h.repo.FindByID(ctx, id)
	if err != nil {
		return terjemahkanError(c, err, "gagal mengambil data user")
	}
	return ok(c, "user ditemukan", user)
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	var req model.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	errs := map[string]string{}
	if req.Username == "" {
		errs["username"] = "wajib diisi"
	}
	if !strings.Contains(req.Email, "@") {
		errs["email"] = "format email tidak valid"
	}
	if len(req.Password) < 8 {
		errs["password"] = "minimal 8 karakter"
	}
	if len(errs) > 0 {
		return failValidation(c, errs)
	}

	// Keunikan username TIDAK diperiksa dengan SELECT lebih dulu.
	// Basis data sudah menjaminnya lewat UNIQUE INDEX, dan pemeriksaan
	// manual justru menyisakan celah bila dua permintaan datang bersamaan.
	baru, err := h.repo.Create(ctx, model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		IsActive: true,
	})
	if err != nil {
		return terjemahkanError(c, err, "gagal menyimpan user")
	}

	return created(c, "user berhasil dibuat", baru,
		"/api/v1/users/"+strconv.Itoa(baru.ID))
}

func (h *UserHandler) Replace(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	id, valid := paramID(c)
	if !valid {
		return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	var req model.ReplaceUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid")
	}

	errs := map[string]string{}
	if strings.TrimSpace(req.Username) == "" {
		errs["username"] = "wajib diisi pada PUT"
	}
	if !strings.Contains(req.Email, "@") {
		errs["email"] = "wajib diisi dan berformat email pada PUT"
	}
	if len(errs) > 0 {
		return failValidation(c, errs)
	}

	hasil, err := h.repo.Update(ctx, model.User{
		ID: id, Username: req.Username, Email: req.Email, IsActive: req.IsActive,
	})
	if err != nil {
		return terjemahkanError(c, err, "gagal memperbarui user")
	}
	return ok(c, "user berhasil diganti seluruhnya", hasil)
}

func (h *UserHandler) Patch(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	id, valid := paramID(c)
	if !valid {
		return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	var req model.PatchUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid")
	}
	if req.Username == nil && req.Email == nil && req.IsActive == nil {
		return fail(c, fiber.StatusBadRequest, "tidak ada field yang diubah")
	}

	// PATCH = baca dulu, ubah seperlunya, lalu simpan kembali.
	// Repository cukup punya satu Update; perbedaan PUT dan PATCH
	// diputuskan di lapisan ini, bukan di lapisan penyimpanan.
	saatIni, err := h.repo.FindByID(ctx, id)
	if err != nil {
		return terjemahkanError(c, err, "gagal mengambil data user")
	}

	if req.Username != nil {
		if strings.TrimSpace(*req.Username) == "" {
			return failValidation(c, map[string]string{"username": "tidak boleh kosong"})
		}
		saatIni.Username = *req.Username
	}
	if req.Email != nil {
		if !strings.Contains(*req.Email, "@") {
			return failValidation(c, map[string]string{"email": "format email tidak valid"})
		}
		saatIni.Email = *req.Email
	}
	if req.IsActive != nil {
		saatIni.IsActive = *req.IsActive
	}

	hasil, err := h.repo.Update(ctx, saatIni)
	if err != nil {
		return terjemahkanError(c, err, "gagal memperbarui user")
	}
	return ok(c, "user berhasil diperbarui sebagian", hasil)
}

func (h *UserHandler) Delete(c *fiber.Ctx) error {
	ctx, cancel := reqCtx(c)
	defer cancel()

	id, valid := paramID(c)
	if !valid {
		return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}
	if err := h.repo.Delete(ctx, id); err != nil {
		return terjemahkanError(c, err, "gagal menghapus user")
	}
	return noContent(c)
}