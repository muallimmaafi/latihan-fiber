package service

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"latihan-fiber/helper"
)

type CourseService struct {
	courses repository.CourseRepository
}

func NewCourseService(courses repository.CourseRepository) *CourseService {
	return &CourseService{courses: courses}
}

// ---------- GET /courses ----------
func (s *CourseService) List(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	courses, err := s.courses.FindAll(ctx)
	if err != nil {
		return helper.Internal(err)
	}
	return helper.Success(c, fiber.StatusOK, "daftar kelas berhasil diambil", courses)
}

// ---------- GET /courses/mine ----------
func (s *CourseService) Mine(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	courses, err := s.courses.FindByStudent(ctx, current.UserID)
	if err != nil {
		return helper.Internal(err)
	}

	total := 0
	for _, course := range courses {
		total += course.Credits
	}
	return helper.Success(c, fiber.StatusOK, "KRS berhasil diambil", fiber.Map{
		"courses":       courses,
		"total_credits": total,
	})
}

// ---------- GET /courses/:id ----------
func (s *CourseService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	course, err := s.courses.FindByID(ctx, id)
	if err != nil {
		return translateCourseError(err)
	}
	return helper.Success(c, fiber.StatusOK, "kelas ditemukan", course)
}

// ---------- POST /courses/:id/enroll ----------
// VERSI NAIF, SENGAJA SALAH. Hanya untuk disaksikan masalahnya, lalu
// diganti seluruhnya pada Langkah 6.
func (s *CourseService) Enroll(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}
	courseID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	// 1. Baca
	course, err := s.courses.FindByID(ctx, courseID)
	if err != nil {
		return translateCourseError(err)
	}

	// 2. Periksa
	if course.EnrolledCount >= course.Quota {
		return translateCourseError(repository.ErrCourseFull)
	}

	// 3. Tulis
	if err := s.courses.InsertEnrollment(ctx, courseID, current.UserID); err != nil {
		return translateCourseError(err)
	}
	if err := s.courses.SetEnrolledCount(ctx, courseID, course.EnrolledCount+1); err != nil {
		return translateCourseError(err)
	}
	course.EnrolledCount++

	return helper.Created(c, "berhasil mendaftar kelas", course,
		"/api/v1/courses/"+strconv.Itoa(courseID))
}

func translateCourseError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound("kelas tidak ditemukan")
	case errors.Is(err, repository.ErrCourseFull):
		return helper.NewError(fiber.StatusConflict, "COURSE_FULL", "kuota kelas sudah penuh")
	case errors.Is(err, repository.ErrDuplicate):
		return helper.NewError(fiber.StatusConflict, "ALREADY_ENROLLED",
			"Anda sudah terdaftar di kelas ini")
	default:
		return helper.Internal(err)
	}
}

var _ = model.Course{} // model dipakai penuh mulai Langkah 6