package service

import (
	"strings"

	"latihan-fiber/app/model"
)

// File ini berisi business rules MURNI: tidak menyentuh fiber.Ctx,
// tidak menyentuh database, dan tidak tahu apa pun tentang HTTP.

// ValidateCreate memeriksa isi permintaan pembuatan user.
// Mengembalikan peta berisi field yang bermasalah; kosong berarti lolos.
func ValidateCreate(req model.CreateUserRequest) map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(req.Username) == "" {
		errs["username"] = "wajib diisi"
	}
	if !isValidEmail(req.Email) {
		errs["email"] = "format email tidak valid"
	}
	if len(req.Password) < 8 {
		errs["password"] = "minimal 8 karakter"
	}
	return errs
}

// ValidateReplace memeriksa isi permintaan PUT.
// Seluruh field wajib ada karena PUT mengganti isi secara keseluruhan.
func ValidateReplace(req model.ReplaceUserRequest) map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(req.Username) == "" {
		errs["username"] = "wajib diisi pada PUT"
	}
	if !isValidEmail(req.Email) {
		errs["email"] = "wajib diisi dan berformat email pada PUT"
	}
	return errs
}

// ApplyPatch menyalin field yang dikirim ke data yang sudah ada.
// Field yang bernilai nil dibiarkan apa adanya.
func ApplyPatch(
	current model.User, req model.PatchUserRequest,
) (model.User, map[string]string) {
	errs := map[string]string{}

	if req.Username != nil {
		if strings.TrimSpace(*req.Username) == "" {
			errs["username"] = "tidak boleh kosong"
		} else {
			current.Username = *req.Username
		}
	}
	if req.Email != nil {
		if !isValidEmail(*req.Email) {
			errs["email"] = "format email tidak valid"
		} else {
			current.Email = *req.Email
		}
	}
	if req.IsActive != nil {
		current.IsActive = *req.IsActive
	}
	return current, errs
}

// IsEmptyPatch menandai permintaan PATCH yang tidak mengubah apa pun.
func IsEmptyPatch(req model.PatchUserRequest) bool {
	return req.Username == nil && req.Email == nil && req.IsActive == nil
}

// CountTotalPages membulatkan ke atas tanpa memakai bilangan pecahan.
func CountTotalPages(total, limit int) int {
	if limit <= 0 {
		return 0
	}
	return (total + limit - 1) / limit
}

// isValidEmail adalah pemeriksaan sederhana, bukan validasi RFC.
// Pemeriksaan yang sungguh-sungguh dibahas pada pertemuan 7.
func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	dot := strings.LastIndex(email, ".")
	return at > 0 && dot > at+1 && dot < len(email)-1
}