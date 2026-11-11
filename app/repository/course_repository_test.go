package repository_test

import (
	"context"
	"os"
	"testing"
	"time"

	"latihan-fiber/app/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPostgresCourseRepo(t *testing.T) (repository.CourseRepository, pgx.Tx, context.Context) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL belum diatur")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		cancel()
		t.Fatalf("gagal membuat koneksi PostgreSQL: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		cancel()
		t.Fatalf("gagal menghubungi PostgreSQL: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("gagal memulai transaksi: %v", err)
	}

	t.Cleanup(func() {
		_ = tx.Rollback(context.Background())
		pool.Close()
		cancel()
	})

	repo := repository.NewCourseRepository(pool).WithTx(tx)

	return repo, tx, ctx
}

func TestCourseRepositoryFindByID(t *testing.T) {
	repo, tx, ctx := newPostgresCourseRepo(t)

	var courseID int

	err := tx.QueryRow(ctx, `
		INSERT INTO courses (code, name, credits, quota)
		VALUES ('IT-TEST-FIND', 'Integration Test Course', 3, 10)
		RETURNING id
	`).Scan(&courseID)

	if err != nil {
		t.Fatalf("gagal membuat data course: %v", err)
	}

	course, err := repo.FindByID(ctx, courseID)
	if err != nil {
		t.Fatalf("FindByID gagal: %v", err)
	}

	if course.ID != courseID {
		t.Errorf("ID tidak sesuai: got %d, want %d", course.ID, courseID)
	}

	if course.Code != "IT-TEST-FIND" {
		t.Errorf("Code tidak sesuai: got %s", course.Code)
	}

	if course.Name != "Integration Test Course" {
		t.Errorf("Name tidak sesuai: got %s", course.Name)
	}
}

func TestCourseRepositoryEnrollmentLifecycle(t *testing.T) {
	repo, tx, ctx := newPostgresCourseRepo(t)

	var userID, courseID int

	err := tx.QueryRow(ctx, `
		INSERT INTO users (username, email, password)
		VALUES ('it_test_student', 'it_test_student@example.com', 'test-password')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("gagal membuat user: %v", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO courses (code, name, credits, quota)
		VALUES ('IT-TEST-ENROLL', 'Enrollment Test', 3, 5)
		RETURNING id
	`).Scan(&courseID)
	if err != nil {
		t.Fatalf("gagal membuat course: %v", err)
	}

	if err := repo.InsertEnrollment(ctx, courseID, userID); err != nil {
		t.Fatalf("InsertEnrollment gagal: %v", err)
	}

	enrolled, err := repo.IsEnrolled(ctx, courseID, userID)
	if err != nil {
		t.Fatalf("IsEnrolled gagal: %v", err)
	}
	if !enrolled {
		t.Error("user seharusnya sudah terdaftar")
	}

	credits, err := repo.SumCredits(ctx, userID)
	if err != nil {
		t.Fatalf("SumCredits gagal: %v", err)
	}
	if credits != 3 {
		t.Errorf("total SKS tidak sesuai: got %d, want 3", credits)
	}

	courses, err := repo.FindByStudent(ctx, userID)
	if err != nil {
		t.Fatalf("FindByStudent gagal: %v", err)
	}
	if len(courses) != 1 {
		t.Errorf("jumlah course tidak sesuai: got %d, want 1", len(courses))
	}

	participants, err := repo.FindParticipants(ctx, courseID)
	if err != nil {
		t.Fatalf("FindParticipants gagal: %v", err)
	}
	if len(participants) != 1 {
		t.Errorf("jumlah peserta tidak sesuai: got %d, want 1", len(participants))
	}

	if _, err := repo.IncrementEnrolled(ctx, courseID); err != nil {
		t.Fatalf("IncrementEnrolled gagal: %v", err)
	}

	if err := repo.DeleteEnrollment(ctx, courseID, userID); err != nil {
		t.Fatalf("DeleteEnrollment gagal: %v", err)
	}

	enrolled, err = repo.IsEnrolled(ctx, courseID, userID)
	if err != nil {
		t.Fatalf("IsEnrolled setelah penghapusan gagal: %v", err)
	}
	if enrolled {
		t.Error("user seharusnya sudah tidak terdaftar")
	}

	if _, err := repo.DecrementEnrolled(ctx, courseID); err != nil {
		t.Fatalf("DecrementEnrolled gagal: %v", err)
	}
}

func TestCourseRepositoryQuotaLimit(t *testing.T) {
	repo, tx, ctx := newPostgresCourseRepo(t)

	var courseID int

	err := tx.QueryRow(ctx, `
		INSERT INTO courses (code, name, credits, quota)
		VALUES ('IT-TEST-QUOTA', 'Quota Test', 3, 1)
		RETURNING id
	`).Scan(&courseID)
	if err != nil {
		t.Fatalf("gagal membuat course: %v", err)
	}

	course, err := repo.IncrementEnrolled(ctx, courseID)
	if err != nil {
		t.Fatalf("IncrementEnrolled pertama gagal: %v", err)
	}

	if course.EnrolledCount != 1 {
		t.Errorf("jumlah peserta tidak sesuai: got %d, want 1", course.EnrolledCount)
	}

	_, err = repo.IncrementEnrolled(ctx, courseID)
	if err != repository.ErrCourseFull {
		t.Errorf("seharusnya menghasilkan ErrCourseFull, got: %v", err)
	}
}

func TestCourseRepositoryDuplicateEnrollment(t *testing.T) {
	repo, tx, ctx := newPostgresCourseRepo(t)

	var userID, courseID int

	err := tx.QueryRow(ctx, `
		INSERT INTO users (username, email, password)
		VALUES ('it_test_duplicate', 'it_test_duplicate@example.com', 'test-password')
		RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("gagal membuat user: %v", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO courses (code, name, credits, quota)
		VALUES ('IT-TEST-DUP', 'Duplicate Test', 3, 5)
		RETURNING id
	`).Scan(&courseID)
	if err != nil {
		t.Fatalf("gagal membuat course: %v", err)
	}

	if err := repo.InsertEnrollment(ctx, courseID, userID); err != nil {
		t.Fatalf("pendaftaran pertama gagal: %v", err)
	}

	err = repo.InsertEnrollment(ctx, courseID, userID)
	if err != repository.ErrDuplicate {
		t.Errorf("seharusnya menghasilkan ErrDuplicate, got: %v", err)
	}
}
