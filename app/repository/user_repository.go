package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"latihan-fiber/app/model"
)

// Sentinel error: error milik lapisan repository, bukan error milik pgx.
// Lapisan atas cukup mengenal dua ini dan tidak perlu tahu basis datanya apa.
var (
	ErrNotFound  = errors.New("data tidak ditemukan")
	ErrDuplicate = errors.New("data sudah ada")
)

// UserRepository adalah KONTRAK penyimpanan data user.
// Perhatikan: tidak ada satu pun kata "SQL" atau "postgres" di sini.
type UserRepository interface {
	FindAll(ctx context.Context, q model.ListQuery) ([]model.User, int, error)
	FindByID(ctx context.Context, id int) (model.User, error)
	Create(ctx context.Context, u model.User) (model.User, error)
	Update(ctx context.Context, u model.User) (model.User, error)
	Delete(ctx context.Context, id int) error
}

// kolomUrut adalah daftar putih: pemetaan dari nilai yang boleh dikirim klien
// ke nama kolom yang sebenarnya. ORDER BY tidak dapat memakai parameter,
// sehingga nama kolom terpaksa disisipkan sebagai teks. Daftar putih inilah
// satu-satunya hal yang mencegah SQL injection di titik ini.
var kolomUrut = map[string]string{
	"id":         "id",
	"username":   "username",
	"email":      "email",
	"created_at": "created_at",
}

type userPostgresRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository mengembalikan interface, bukan struct konkret.
func NewUserRepository(pool *pgxpool.Pool) UserRepository {
	return &userPostgresRepository{pool: pool}
}

// buildFilter menyusun bagian WHERE beserta argumennya.
// Nilai dari klien SELALU menjadi argumen ($1, $2, ...), tidak pernah
// disambung langsung ke dalam teks SQL.
func buildFilter(q model.ListQuery) (string, []any) {
	where := " WHERE 1 = 1"
	args := []any{}

	if q.Search != "" {
		where += fmt.Sprintf(" AND (username ILIKE $%d OR email ILIKE $%d)",
			len(args)+1, len(args)+1)
		args = append(args, "%"+q.Search+"%")
	}
	if q.IsActive != nil {
		where += fmt.Sprintf(" AND is_active = $%d", len(args)+1)
		args = append(args, *q.IsActive)
	}
	return where, args
}

func (r *userPostgresRepository) FindAll(
	ctx context.Context, q model.ListQuery,
) ([]model.User, int, error) {
	where, args := buildFilter(q)

	// 1) Hitung total sebelum dipenggal, untuk keperluan meta.
	var total int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users"+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("menghitung user: %w", err)
	}

	// 2) Ambil satu halaman saja. Penyaringan, pengurutan, dan pemenggalan
	// dikerjakan basis data, bukan oleh Go.
	arah := "ASC"
	if q.Order == "desc" {
		arah = "DESC"
	}

	sqlText := fmt.Sprintf(
		`SELECT id, username, email, password, is_active, created_at
		 FROM users%s
		 ORDER BY %s %s
		 LIMIT $%d OFFSET $%d`,
		where, kolomUrut[q.Sort], arah, len(args)+1, len(args)+2,
	)
	args = append(args, q.Limit, q.Offset())

	rows, err := r.pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("mengambil daftar user: %w", err)
	}
	defer rows.Close()

	hasil := []model.User{}
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Password,
			&u.IsActive, &u.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("membaca baris user: %w", err)
		}
		hasil = append(hasil, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("membaca hasil query: %w", err)
	}
	return hasil, total, nil
}

func (r *userPostgresRepository) FindByID(
	ctx context.Context, id int,
) (model.User, error) {
	var u model.User
	err := r.pool.QueryRow(ctx,
		`SELECT id, username, email, password, is_active, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &u.IsActive, &u.CreatedAt)
	if err != nil {
		// pgx.ErrNoRows diterjemahkan menjadi error milik kita sendiri.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, fmt.Errorf("mengambil user: %w", err)
	}
	return u, nil
}

func (r *userPostgresRepository) Create(
	ctx context.Context, u model.User,
) (model.User, error) {
	// RETURNING membuat id dan created_at hasil buatan basis data
	// langsung ikut kembali, tanpa perlu query kedua.
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password, is_active)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		u.Username, u.Email, u.Password, u.IsActive,
	).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return model.User{}, ErrDuplicate
		}
		return model.User{}, fmt.Errorf("menyimpan user: %w", err)
	}
	return u, nil
}

func (r *userPostgresRepository) Update(
	ctx context.Context, u model.User,
) (model.User, error) {
	// RETURNING mengembalikan baris hasil perubahan dalam satu perjalanan,
	// sehingga field yang tidak ikut diubah (created_at) tetap terisi benar.
	err := r.pool.QueryRow(ctx,
		`UPDATE users SET username = $1, email = $2, is_active = $3
		 WHERE id = $4
		 RETURNING id, username, email, password, is_active, created_at`,
		u.Username, u.Email, u.IsActive, u.ID,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Password, &u.IsActive, &u.CreatedAt)
	if err != nil {
		// Tidak ada baris yang dikembalikan berarti id-nya memang tidak ada.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return model.User{}, ErrDuplicate
		}
		return model.User{}, fmt.Errorf("memperbarui user: %w", err)
	}
	return u, nil
}

func (r *userPostgresRepository) Delete(ctx context.Context, id int) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("menghapus user: %w", err)
	}
	// Perintah berhasil dijalankan, tetapi tidak ada baris yang terkena.
	// Artinya id-nya memang tidak ada.
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation memeriksa apakah error berasal dari pelanggaran
// batasan UNIQUE. Kode 23505 adalah kode resmi PostgreSQL untuk itu.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}