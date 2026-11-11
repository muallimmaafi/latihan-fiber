package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

const refreshTokenBytes = 32

type AuthService struct {
	users      repository.UserRepository
	tokens     repository.TokenRepository
	jwt        *helper.JWTManager
	perms      *helper.PermissionSet
	refreshTTL time.Duration
}

func NewAuthService(
	users repository.UserRepository,
	tokens repository.TokenRepository,
	jwtManager *helper.JWTManager,
	perms *helper.PermissionSet,
	refreshTTL time.Duration,
) *AuthService {
	return &AuthService{
		users: users, tokens: tokens, jwt: jwtManager,
		perms: perms, refreshTTL: refreshTTL,
	}
}


// Register godoc
// @Summary Registrasi pengguna
// @Description Mendaftarkan pengguna baru. Role otomatis ditetapkan sebagai user.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body model.RegisterRequest true "Data registrasi pengguna"
// @Success 201 {object} map[string]interface{} "Pendaftaran berhasil"
// @Failure 400 {object} map[string]interface{} "Request tidak valid"
// @Failure 409 {object} map[string]interface{} "Username atau email sudah digunakan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Router /auth/register [post]
func (s *AuthService) Register(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	// Password DI-HASH sebelum menyentuh database.
	hashed, err := helper.HashPassword(req.Password)
	if err != nil {
		return helper.Internal(err)
	}

	// Role selalu ditentukan server, tidak pernah diambil dari request.
	newUser, err := s.users.Create(ctx, model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hashed,
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		return translateError(err, "user")
	}

	return helper.Created(c, "pendaftaran berhasil", newUser,
		"/api/v1/users/"+strconv.Itoa(newUser.ID))
}

// Login godoc
// @Summary Login pengguna
// @Description Memverifikasi username dan password, kemudian menghasilkan access token dan refresh token.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body model.LoginRequest true "Kredensial login"
// @Success 200 {object} map[string]interface{} "Login berhasil"
// @Failure 400 {object} map[string]interface{} "Request tidak valid"
// @Failure 401 {object} map[string]interface{} "Username atau password salah"
// @Failure 403 {object} map[string]interface{} "Akun dinonaktifkan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Router /auth/login [post]
func (s *AuthService) Login(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	user, err := s.users.FindByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			// Kegagalan database BUKAN "username salah": jangan disamarkan.
			return helper.Internal(err)
		}
		// Username tidak ditemukan. Tetap jalankan pemeriksaan palsu agar
		// waktu tanggapnya mirip dengan kasus password salah, dan jawab
		// dengan pesan yang SAMA PERSIS.
		helper.VerifyDummyPassword(req.Password)
		return helper.Unauthorized("username atau password salah")
	}

	if !helper.VerifyPassword(user.Password, req.Password) {
		return helper.Unauthorized("username atau password salah")
	}

	if !user.IsActive {
		return helper.Forbidden("akun dinonaktifkan")
	}

	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return helper.Internal(err)
	}
	return helper.Success(c, fiber.StatusOK, "login berhasil", pair)
}

// Refresh godoc
// @Summary Memperbarui token
// @Description Memperbarui access token dan refresh token menggunakan refresh token yang masih valid. Refresh token lama akan dicabut.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body model.RefreshRequest true "Refresh token"
// @Success 200 {object} map[string]interface{} "Token berhasil diperbarui"
// @Failure 400 {object} map[string]interface{} "Request tidak valid"
// @Failure 401 {object} map[string]interface{} "Refresh token tidak valid atau kedaluwarsa"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Router /auth/refresh [post]
func (s *AuthService) Refresh(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		return helper.BadRequest("refresh_token wajib diisi")
	}

	hash := helper.SHA256Hex(req.RefreshToken)

	stored, err := s.tokens.FindActive(ctx, hash)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return helper.Internal(err)
		}
		return helper.Unauthorized("refresh token tidak valid atau sudah kedaluwarsa")
	}

	user, err := s.users.FindByID(ctx, stored.UserID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return helper.Internal(err)
		}
		return helper.Unauthorized("akun tidak dapat dipakai")
	}
	if !user.IsActive {
		return helper.Unauthorized("akun tidak dapat dipakai")
	}

	// ROTASI: token lama langsung dicabut dan diganti yang baru.
	if err := s.tokens.Revoke(ctx, hash); err != nil {
		return helper.Internal(err)
	}

	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return helper.Internal(err)
	}
	return helper.Success(c, fiber.StatusOK, "token berhasil diperbarui", pair)
}

// Logout godoc
// @Summary Logout pengguna
// @Description Mengakhiri sesi pengguna dengan mencabut refresh token.
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body model.RefreshRequest true "Refresh token yang akan dicabut"
// @Success 200 {object} map[string]interface{} "Logout berhasil"
// @Failure 400 {object} map[string]interface{} "Request tidak valid"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /auth/logout [post]
func (s *AuthService) Logout(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	if strings.TrimSpace(req.RefreshToken) != "" {
		// Kegagalan mencabut tidak dilaporkan ke client: dari sudut
		// pandang pemakai, logout harus selalu berhasil.
		_ = s.tokens.Revoke(ctx, helper.SHA256Hex(req.RefreshToken))
	}
	return helper.Success(c, fiber.StatusOK, "logout berhasil", nil)
}

// Me godoc
// @Summary Mendapatkan profil pengguna
// @Description Mengambil informasi profil dan daftar permission pengguna yang sedang login.
// @Tags Authentication
// @Produce json
// @Success 200 {object} map[string]interface{} "Profil berhasil diambil"
// @Failure 401 {object} map[string]interface{} "Belum terautentikasi atau user tidak ditemukan"
// @Failure 500 {object} map[string]interface{} "Kesalahan server"
// @Security BearerAuth
// @Router /auth/me [get]
func (s *AuthService) Me(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	authUser, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	user, err := s.users.FindByID(ctx, authUser.UserID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			return helper.Internal(err)
		}
		return helper.Unauthorized("user tidak ditemukan")
	}

	return helper.Success(c, fiber.StatusOK, "profil berhasil diambil", fiber.Map{
		"user":        user,
		"permissions": s.perms.PermissionsOf(user.Role),
	})
}

// issueTokenPair membuat access token dan refresh token sekaligus.
func (s *AuthService) issueTokenPair(
	ctx context.Context, user model.User,
) (model.TokenPair, error) {
	accessToken, err := s.jwt.GenerateAccess(user)
	if err != nil {
		return model.TokenPair{}, err
	}

	refreshToken, err := helper.RandomToken(refreshTokenBytes)
	if err != nil {
		return model.TokenPair{}, err
	}

	// Yang disimpan hash-nya, bukan tokennya.
	err = s.tokens.Save(ctx, model.RefreshToken{
		UserID:    user.ID,
		TokenHash: helper.SHA256Hex(refreshToken),
		ExpiresAt: time.Now().Add(s.refreshTTL),
	})
	if err != nil {
		return model.TokenPair{}, err
	}

	return model.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.jwt.AccessTTL().Seconds()),
	}, nil
}
