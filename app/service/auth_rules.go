package service

import (
	"strings"
	"unicode"

	"latihan-fiber/app/model"
)

const minPasswordLength = 8

func ValidateRegister(req model.RegisterRequest) map[string]string {
	errs := map[string]string{}

	username := strings.TrimSpace(req.Username)
	switch {
	case username == "":
		errs["username"] = "wajib diisi"
	case len(username) < 3:
		errs["username"] = "minimal 3 karakter"
	case !isValidUsername(username):
		errs["username"] = "hanya boleh huruf, angka, titik, dan garis bawah"
	}

	if !isValidEmail(req.Email) {
		errs["email"] = "format email tidak valid"
	}
	if msg := checkPasswordStrength(req.Password); msg != "" {
		errs["password"] = msg
	}
	return errs
}

// ValidateLogin hanya memeriksa kelengkapan, BUKAN kekuatan password.
// Aturan kekuatan tidak diberlakukan di sini karena password lama
// mungkin dibuat sebelum aturannya berubah.
func ValidateLogin(req model.LoginRequest) map[string]string {
	errs := map[string]string{}
	if strings.TrimSpace(req.Username) == "" {
		errs["username"] = "wajib diisi"
	}
	if req.Password == "" {
		errs["password"] = "wajib diisi"
	}
	return errs
}

func checkPasswordStrength(password string) string {
	if len(password) < minPasswordLength {
		return "minimal 8 karakter"
	}

	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return "harus memuat huruf dan angka"
	}

	// Daftar ini sengaja sangat pendek. Sistem sungguhan memakai daftar
	// berisi jutaan password yang pernah bocor.
	weak := map[string]bool{
		"password1": true, "12345678": true, "qwerty123": true,
		"admin123": true, "password123": true,
	}
	if weak[strings.ToLower(password)] {
		return "password terlalu umum"
	}
	return ""
}

func isValidUsername(username string) bool {
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' {
			return false
		}
	}
	return true
}