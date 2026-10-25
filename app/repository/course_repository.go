package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"latihan-fiber/app/model"
)

var (
	ErrCourseFull  = errors.New("kuota kelas sudah penuh")
	ErrNotEnrolled = errors.New("tidak terdaftar di kelas ini")
)

type CourseRepository interface {
	// WithTx mengembalikan salinan repository yang menjalankan seluruh
	// query-nya di dalam transaction tx.
	WithTx(tx DBTX) CourseRepository

	FindAll(ctx context.Context) ([]model.Course, error)
	FindByID(ctx context.Context, id int) (model.Course, error)
	FindByStudent(ctx context.Context, userID int) ([]model.Course, error)
	IsEnrolled(ctx context.Context, courseID, userID int) (bool, error)

	LockStudent(ctx context.Context, userID int) error
	SumCredits(ctx context.Context, userID int) (int, error)
	IncrementEnrolled(ctx context.Context, courseID int) (model.Course, error)
	DecrementEnrolled(ctx context.Context, courseID int) (model.Course, error)
	InsertEnrollment(ctx context.Context, courseID, userID int) error
	DeleteEnrollment(ctx context.Context, courseID, userID int) error
}

const courseColumns = `id, code, name, credits, quota, enrolled_count, version, created_at`

type coursePostgresRepository struct{ db DBTX }

// NewCourseRepository menerima DBTX. Pada main.go yang dikirim adalah
// pool, sehingga repository ini bekerja di luar transaction sampai
// WithTx dipanggil.
func NewCourseRepository(db DBTX) CourseRepository {
	return &coursePostgresRepository{db: db}
}

func (r *coursePostgresRepository) WithTx(tx DBTX) CourseRepository {
	return &coursePostgresRepository{db: tx}
}

func scanCourse(row pgx.Row) (model.Course, error) {
	var c model.Course
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.Credits, &c.Quota,
		&c.EnrolledCount, &c.Version, &c.CreatedAt)
	return c, err
}

func (r *coursePostgresRepository) collect(rows pgx.Rows, err error) ([]model.Course, error) {
	if err != nil {
		return nil, fmt.Errorf("mengambil daftar kelas: %w", err)
	}
	defer rows.Close()

	result := []model.Course{}
	for rows.Next() {
		c, err := scanCourse(rows)
		if err != nil {
			return nil, fmt.Errorf("membaca row kelas: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *coursePostgresRepository) FindAll(ctx context.Context) ([]model.Course, error) {
	return r.collect(r.db.Query(ctx,
		"SELECT "+courseColumns+" FROM courses ORDER BY code"))
}

func (r *coursePostgresRepository) FindByID(ctx context.Context, id int) (model.Course, error) {
	c, err := scanCourse(r.db.QueryRow(ctx,
		"SELECT "+courseColumns+" FROM courses WHERE id = $1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Course{}, ErrNotFound
		}
		return model.Course{}, fmt.Errorf("mengambil kelas: %w", err)
	}
	return c, nil
}

func (r *coursePostgresRepository) FindByStudent(
	ctx context.Context, userID int,
) ([]model.Course, error) {
	return r.collect(r.db.Query(ctx,
		`SELECT c.id, c.code, c.name, c.credits, c.quota, c.enrolled_count,
		        c.version, c.created_at
		 FROM courses c
		 JOIN enrollments e ON e.course_id = c.id
		 WHERE e.user_id = $1
		 ORDER BY c.code`, userID))
}

// LockStudent mengunci row milik satu mahasiswa sampai transaction selesai.
//
// Row users dipakai sebagai "gembok KRS": selama satu transaction memegang
// kunci ini, transaction lain milik mahasiswa YANG SAMA harus antre.
// Mahasiswa lain tidak terpengaruh, karena row yang dikunci berbeda.
//
// Tanpa FOR UPDATE, SELECT ini tidak mengunci apa pun dan pemeriksaan
// batas SKS sesudahnya dapat diakali dengan request yang serentak.
func (r *coursePostgresRepository) LockStudent(ctx context.Context, userID int) error {
	var id int
	err := r.db.QueryRow(ctx,
		"SELECT id FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("mengunci data mahasiswa: %w", err)
	}
	return nil
}

func (r *coursePostgresRepository) SumCredits(ctx context.Context, userID int) (int, error) {
	var total int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(c.credits), 0)
		 FROM enrollments e
		 JOIN courses c ON c.id = e.course_id
		 WHERE e.user_id = $1`, userID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("menghitung SKS: %w", err)
	}
	return total, nil
}

// IncrementEnrolled mengambil satu kursi secara ATOMIK.
//
// Pemeriksaan kuota dan penambahan jumlah peserta terjadi dalam satu
// pernyataan UPDATE. PostgreSQL mengunci row tersebut selama UPDATE
// berjalan, sehingga dua request yang datang bersamaan tidak mungkin
// sama-sama melihat "masih ada satu kursi kosong".
func (r *coursePostgresRepository) IncrementEnrolled(
	ctx context.Context, courseID int,
) (model.Course, error) {
	c, err := scanCourse(r.db.QueryRow(ctx,
		`UPDATE courses SET enrolled_count = enrolled_count + 1
		 WHERE id = $1 AND enrolled_count < quota
		 RETURNING `+courseColumns, courseID))
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Course{}, fmt.Errorf("menambah peserta: %w", err)
	}

	// Tidak ada row yang berubah. Dua kemungkinan: kelasnya tidak ada,
	// atau kelasnya ada tetapi sudah penuh. Bedakan keduanya.
	if _, findErr := r.FindByID(ctx, courseID); findErr != nil {
		return model.Course{}, findErr
	}
	return model.Course{}, ErrCourseFull
}

func (r *coursePostgresRepository) DecrementEnrolled(
	ctx context.Context, courseID int,
) (model.Course, error) {
	c, err := scanCourse(r.db.QueryRow(ctx,
		`UPDATE courses SET enrolled_count = enrolled_count - 1
		 WHERE id = $1
		 RETURNING `+courseColumns, courseID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Course{}, ErrNotFound
		}
		return model.Course{}, fmt.Errorf("mengurangi peserta: %w", err)
	}
	return c, nil
}

func (r *coursePostgresRepository) InsertEnrollment(
	ctx context.Context, courseID, userID int,
) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO enrollments (course_id, user_id) VALUES ($1, $2)", courseID, userID)
	if err != nil {
		if pgErrorCode(err) == codeUniqueViolation {
			return ErrDuplicate
		}
		return fmt.Errorf("mencatat pendaftaran: %w", err)
	}
	return nil
}

// DeleteEnrollment menghapus pendaftaran, lalu MEMERIKSA apakah memang
// ada yang terhapus. DELETE yang tidak menemukan row bukan error menurut
// PostgreSQL: ia sukses dengan nol row. Bila tidak diperiksa, kode
// sesudahnya akan mengurangi jumlah peserta untuk pendaftaran yang
// sebenarnya tidak pernah ada.
func (r *coursePostgresRepository) DeleteEnrollment(
	ctx context.Context, courseID, userID int,
) error {
	tag, err := r.db.Exec(ctx,
		"DELETE FROM enrollments WHERE course_id = $1 AND user_id = $2", courseID, userID)
	if err != nil {
		return fmt.Errorf("menghapus pendaftaran: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotEnrolled
	}
	return nil
}

// IsEnrolled dipakai layer lain, misalnya layanan ulasan di MongoDB, yang
// perlu tahu apakah seorang mahasiswa terdaftar di sebuah kelas.
func (r *coursePostgresRepository) IsEnrolled(
	ctx context.Context, courseID, userID int,
) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM enrollments WHERE course_id = $1 AND user_id = $2)",
		courseID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("memeriksa pendaftaran: %w", err)
	}
	return exists, nil
}