package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"latihan-fiber/app/model"
)

type CourseRepository interface {
	// WithTx mengembalikan salinan repository yang menjalankan seluruh
	// query-nya di dalam transaction tx.
	WithTx(tx DBTX) CourseRepository

	FindAll(ctx context.Context) ([]model.Course, error)
	FindByID(ctx context.Context, id int) (model.Course, error)
	FindByStudent(ctx context.Context, userID int) ([]model.Course, error)
	InsertEnrollment(ctx context.Context, courseID, userID int) error
	SetEnrolledCount(ctx context.Context, courseID, count int) error
}

const courseColumns = `id, code, name, credits, quota, enrolled_count, version, created_at`

var ErrCourseFull = errors.New("kuota kelas sudah penuh")

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

// SetEnrolledCount: VERSI NAIF. Nilai baru dihitung di Go, lalu
// dituliskan menimpa nilai lama.
func (r *coursePostgresRepository) SetEnrolledCount(
	ctx context.Context, courseID, count int,
) error {
	_, err := r.db.Exec(ctx,
		"UPDATE courses SET enrolled_count = $1 WHERE id = $2", count, courseID)
	if err != nil {
		return fmt.Errorf("menyimpan jumlah peserta: %w", err)
	}
	return nil
}