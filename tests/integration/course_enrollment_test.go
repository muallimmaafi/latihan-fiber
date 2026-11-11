package integration_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/app/service"
	"latihan-fiber/config"
	"latihan-fiber/helper"
	"latihan-fiber/route"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// Memuat konfigurasi khusus database pengujian.
	_ = godotenv.Load("../../.env.test")

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL belum diatur; integration test dilewati")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("gagal membuat koneksi database pengujian: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("gagal terhubung ke database pengujian: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

func newTestApp(t *testing.T, pool *pgxpool.Pool, userID int, username string) (*fiber.App, string) {

	jwt := helper.NewJWTManager(
		"integration-test-secret-key-at-least-32-characters",
		"latihan-fiber-integration",
		time.Hour,
	)

	token, err := jwt.GenerateAccess(model.User{
		ID:       userID,
		Username: username,
		Role:     "user",
	})
	if err != nil {
		t.Fatalf("gagal membuat token pengujian: %v", err)
	}

	courseRepo := repository.NewCourseRepository(pool)
	outboxRepo := repository.NewOutboxRepository(pool)
	txManager := repository.NewTxManager(pool)
	courseService := service.NewCourseService(courseRepo, outboxRepo, txManager)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	app := config.NewApp(logger, route.Dependencies{
		Pool:          pool,
		JWT:           jwt,
		CourseService: courseService,
	})

	return app, token
}

func createTestUser(t *testing.T, pool *pgxpool.Pool, suffix string) (int, string) {
	t.Helper()

	username := "it_" + suffix
	email := username + "@example.test"

	var userID int
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO users (username, email, password, role)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		username, email, "integration-test-password", "user",
	).Scan(&userID)

	if err != nil {
		t.Fatalf("gagal membuat user pengujian: %v", err)
	}

	return userID, username
}

func createTestCourse(
	t *testing.T,
	pool *pgxpool.Pool,
	suffix string,
	quota int,
	enrolledCount int,
) int {
	t.Helper()

	var courseID int
	err := pool.QueryRow(
		context.Background(),
		`INSERT INTO courses (code, name, credits, quota, enrolled_count)
		 VALUES ($1, $2, 3, $3, $4)
		 RETURNING id`,
		"IT"+suffix, "Integration Test "+suffix, quota, enrolledCount,
	).Scan(&courseID)

	if err != nil {
		t.Fatalf("gagal membuat course pengujian: %v", err)
	}

	return courseID
}

func enrollTestUser(t *testing.T, pool *pgxpool.Pool, courseID, userID int) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO enrollments (course_id, user_id) VALUES ($1, $2)`,
		courseID, userID,
	)
	if err != nil {
		t.Fatalf("gagal membuat enrollment pengujian: %v", err)
	}
}

func registerFixtureCleanup(
	t *testing.T,
	pool *pgxpool.Pool,
	userIDs []int,
	courseIDs []int,
) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, userID := range userIDs {
			_, _ = pool.Exec(ctx,
				`DELETE FROM outbox WHERE payload->>'user_id' = $1`,
				strconv.Itoa(userID),
			)
		}

		for _, courseID := range courseIDs {
			_, _ = pool.Exec(ctx,
				`DELETE FROM enrollments WHERE course_id = $1`,
				courseID,
			)
		}

		for _, userID := range userIDs {
			_, _ = pool.Exec(ctx,
				`DELETE FROM users WHERE id = $1`,
				userID,
			)
		}

		for _, courseID := range courseIDs {
			_, _ = pool.Exec(ctx,
				`DELETE FROM courses WHERE id = $1`,
				courseID,
			)
		}
	})
}

func TestSwapFullCourse(t *testing.T) {
	pool := newTestPool(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)

	studentID, username := createTestUser(t, pool, suffix+"a")
	occupantID, _ := createTestUser(t, pool, suffix+"b")

	fromCourseID := createTestCourse(t, pool, suffix+"a", 5, 1)
	toCourseID := createTestCourse(t, pool, suffix+"b", 1, 1)

	enrollTestUser(t, pool, fromCourseID, studentID)
	enrollTestUser(t, pool, toCourseID, occupantID)

	registerFixtureCleanup(
		t, pool,
		[]int{studentID, occupantID},
		[]int{fromCourseID, toCourseID},
	)

	app, token := newTestApp(t, pool, studentID, username)

	body := fmt.Sprintf(
		`{"from_course_id":%d,"to_course_id":%d}`,
		fromCourseID, toCourseID,
	)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/enrollments/swap",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("gagal mengirim request swap: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status seharusnya 409, tetapi mendapat %d", resp.StatusCode)
	}

	responseBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(responseBody), "COURSE_FULL") {
		t.Fatalf("response seharusnya memuat COURSE_FULL, mendapat: %s", responseBody)
	}

	var sourceEnrollmentCount, targetEnrollmentCount int
	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		fromCourseID, studentID,
	).Scan(&sourceEnrollmentCount)
	if err != nil {
		t.Fatalf("gagal memeriksa enrollment kelas asal: %v", err)
	}

	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		toCourseID, studentID,
	).Scan(&targetEnrollmentCount)
	if err != nil {
		t.Fatalf("gagal memeriksa enrollment kelas tujuan: %v", err)
	}

	if sourceEnrollmentCount != 1 || targetEnrollmentCount != 0 {
		t.Fatalf(
			"enrollment berubah: kelas asal=%d, kelas tujuan=%d",
			sourceEnrollmentCount, targetEnrollmentCount,
		)
	}

	var sourceCount, targetCount int
	err = pool.QueryRow(
		context.Background(),
		`SELECT enrolled_count FROM courses WHERE id = $1`,
		fromCourseID,
	).Scan(&sourceCount)
	if err != nil {
		t.Fatalf("gagal memeriksa jumlah peserta kelas asal: %v", err)
	}

	err = pool.QueryRow(
		context.Background(),
		`SELECT enrolled_count FROM courses WHERE id = $1`,
		toCourseID,
	).Scan(&targetCount)
	if err != nil {
		t.Fatalf("gagal memeriksa jumlah peserta kelas tujuan: %v", err)
	}

	if sourceCount != 1 || targetCount != 1 {
		t.Fatalf(
			"jumlah peserta berubah: kelas asal=%d, kelas tujuan=%d",
			sourceCount, targetCount,
		)
	}

	var swappedEvents int
	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM outbox
		 WHERE event_type = $1 AND payload->>'user_id' = $2`,
		model.EventEnrollmentSwapped, strconv.Itoa(studentID),
	).Scan(&swappedEvents)
	if err != nil {
		t.Fatalf("gagal memeriksa event outbox: %v", err)
	}

	if swappedEvents != 0 {
		t.Fatalf("seharusnya tidak ada event enrollment.swapped, ditemukan %d", swappedEvents)
	}
}

func TestDropEnrollment(t *testing.T) {
	pool := newTestPool(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)

	userID, username := createTestUser(t, pool, suffix+"c")
	courseID := createTestCourse(t, pool, suffix+"c", 5, 1)
	enrollTestUser(t, pool, courseID, userID)

	registerFixtureCleanup(t, pool, []int{userID}, []int{courseID})

	app, token := newTestApp(t, pool, userID, username)

	req := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/courses/%d/enroll", courseID),
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("gagal mengirim request drop: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status seharusnya 204, tetapi mendapat %d: %s", resp.StatusCode, body)
	}

	var enrollmentCount int
	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		courseID, userID,
	).Scan(&enrollmentCount)
	if err != nil {
		t.Fatalf("gagal memeriksa enrollment: %v", err)
	}

	if enrollmentCount != 0 {
		t.Fatalf("enrollment seharusnya terhapus, tetapi masih ditemukan %d", enrollmentCount)
	}

	var enrolledCount int
	err = pool.QueryRow(
		context.Background(),
		`SELECT enrolled_count FROM courses WHERE id = $1`,
		courseID,
	).Scan(&enrolledCount)
	if err != nil {
		t.Fatalf("gagal memeriksa enrolled_count: %v", err)
	}

	if enrolledCount != 0 {
		t.Fatalf("enrolled_count seharusnya 0, tetapi mendapat %d", enrolledCount)
	}

	var droppedEvents int
	err = pool.QueryRow(
		context.Background(),
		`SELECT COUNT(*) FROM outbox
		 WHERE event_type = $1 AND payload->>'user_id' = $2`,
		model.EventEnrollmentDropped, strconv.Itoa(userID),
	).Scan(&droppedEvents)
	if err != nil {
		t.Fatalf("gagal memeriksa event outbox: %v", err)
	}

	if droppedEvents != 1 {
		t.Fatalf("seharusnya ada tepat 1 event enrollment.dropped, ditemukan %d", droppedEvents)
	}
}
