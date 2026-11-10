package service

import (
	"context"
	"sync"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
	"sort"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type fakeCourseRepo struct {
	mu          sync.Mutex
	courses     map[int]model.Course
	users       map[int]model.User
	enrollments map[int]map[int]bool
}

func newFakeCourseRepo() *fakeCourseRepo {
	return &fakeCourseRepo{
		courses:     make(map[int]model.Course),
		users:       make(map[int]model.User),
		enrollments: make(map[int]map[int]bool),
	}
}

func (f *fakeCourseRepo) WithTx(tx repository.DBTX) repository.CourseRepository {
	return f
}

func (f *fakeCourseRepo) FindAll(ctx context.Context) ([]model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]model.Course, 0, len(f.courses))
	for _, course := range f.courses {
		result = append(result, course)
	}
	return result, nil
}

func (f *fakeCourseRepo) FindByID(ctx context.Context, id int) (model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	course, ok := f.courses[id]
	if !ok {
		return model.Course{}, repository.ErrNotFound
	}
	return course, nil
}

func (f *fakeCourseRepo) UpdateQuota(ctx context.Context, id, quota int) (model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	course, ok := f.courses[id]
	if !ok {
		return model.Course{}, repository.ErrNotFound
	}

	course.Quota = quota
	course.Version++
	f.courses[id] = course
	return course, nil
}

func (f *fakeCourseRepo) FindByStudent(ctx context.Context, userID int) ([]model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]model.Course, 0)
	for courseID, students := range f.enrollments {
		if students[userID] {
			if course, ok := f.courses[courseID]; ok {
				result = append(result, course)
			}
		}
	}
	return result, nil
}

func (f *fakeCourseRepo) FindParticipants(ctx context.Context, courseID int) ([]model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]model.User, 0)
	for userID, enrolled := range f.enrollments[courseID] {
		if enrolled {
			if user, ok := f.users[userID]; ok {
				result = append(result, user)
			}
		}
	}
	return result, nil
}

func (f *fakeCourseRepo) IsEnrolled(ctx context.Context, courseID, userID int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.enrollments[courseID][userID], nil
}

func (f *fakeCourseRepo) LockStudent(ctx context.Context, userID int) error {
	return nil
}

func (f *fakeCourseRepo) SumCredits(ctx context.Context, userID int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := 0
	for courseID, students := range f.enrollments {
		if students[userID] {
			total += f.courses[courseID].Credits
		}
	}
	return total, nil
}

func (f *fakeCourseRepo) IncrementEnrolled(ctx context.Context, courseID int) (model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	course, ok := f.courses[courseID]
	if !ok {
		return model.Course{}, repository.ErrNotFound
	}
	if course.EnrolledCount >= course.Quota {
		return model.Course{}, repository.ErrCourseFull
	}

	course.EnrolledCount++
	course.Version++
	f.courses[courseID] = course
	return course, nil
}

func (f *fakeCourseRepo) DecrementEnrolled(ctx context.Context, courseID int) (model.Course, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	course, ok := f.courses[courseID]
	if !ok {
		return model.Course{}, repository.ErrNotFound
	}

	if course.EnrolledCount > 0 {
		course.EnrolledCount--
		course.Version++
	}
	f.courses[courseID] = course
	return course, nil
}

func (f *fakeCourseRepo) InsertEnrollment(ctx context.Context, courseID, userID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.enrollments[courseID] == nil {
		f.enrollments[courseID] = make(map[int]bool)
	}
	if f.enrollments[courseID][userID] {
		return repository.ErrDuplicate
	}

	f.enrollments[courseID][userID] = true
	return nil
}

func (f *fakeCourseRepo) DeleteEnrollment(ctx context.Context, courseID, userID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.enrollments[courseID][userID] {
		return repository.ErrNotEnrolled
	}

	delete(f.enrollments[courseID], userID)
	return nil
}

type fakeReviewRepo struct {
	mu        sync.Mutex
	reviews   map[string]model.Review
	helpfulBy map[string]map[int]bool
}

func newFakeReviewRepo() *fakeReviewRepo {
	return &fakeReviewRepo{
		reviews: make(map[string]model.Review),
	}
}

func fakeReviewID(id string) error {
	_, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return repository.ErrInvalidID
	}
	return nil
}

func cloneReview(r model.Review) model.Review {
	r.Tags = append([]string{}, r.Tags...)
	r.Replies = append([]model.Reply{}, r.Replies...)
	return r
}

func (f *fakeReviewRepo) EnsureIndexes(ctx context.Context) error {
	return nil
}

func (f *fakeReviewRepo) Create(
	ctx context.Context, r model.Review,
) (model.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.reviews {
		if existing.CourseID == r.CourseID && existing.UserID == r.UserID {
			return model.Review{}, repository.ErrDuplicate
		}
	}

	now := time.Now().UTC().Truncate(time.Millisecond)

	r.ID = bson.NewObjectID().Hex()
	r.CreatedAt = now
	r.UpdatedAt = now
	r.HelpfulCount = 0
	r.Tags = append([]string{}, r.Tags...)
	r.Replies = []model.Reply{}

	f.reviews[r.ID] = cloneReview(r)
	return cloneReview(r), nil
}

func (f *fakeReviewRepo) FindByID(
	ctx context.Context, id string,
) (model.Review, error) {
	if err := fakeReviewID(id); err != nil {
		return model.Review{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.reviews[id]
	if !ok {
		return model.Review{}, repository.ErrNotFound
	}

	return cloneReview(r), nil
}

func (f *fakeReviewRepo) FindByCourse(
	ctx context.Context, q model.ReviewQuery,
) ([]model.Review, error) {
	if q.Before != "" {
		if err := fakeReviewID(q.Before); err != nil {
			return nil, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]model.Review, 0)

	for _, r := range f.reviews {
		if r.CourseID != q.CourseID {
			continue
		}

		if q.Tag != "" {
			found := false
			for _, tag := range r.Tags {
				if tag == q.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		if q.Before != "" && r.ID >= q.Before {
			continue
		}

		result = append(result, cloneReview(r))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID > result[j].ID
	})

	if q.Limit > 0 && len(result) > q.Limit {
		result = result[:q.Limit]
	}

	return result, nil
}

func (f *fakeReviewRepo) UpdateOwn(
	ctx context.Context,
	id string,
	userID int,
	req model.UpdateReviewRequest,
) (model.Review, error) {
	if err := fakeReviewID(id); err != nil {
		return model.Review{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.reviews[id]
	if !ok {
		return model.Review{}, repository.ErrNotFound
	}

	if r.UserID != userID {
		return model.Review{}, repository.ErrNotOwner
	}

	if req.Rating != nil {
		r.Rating = *req.Rating
	}
	if req.Comment != nil {
		r.Comment = *req.Comment
	}
	if req.Tags != nil {
		r.Tags = append([]string{}, (*req.Tags)...)
	}

	r.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	f.reviews[id] = cloneReview(r)

	return cloneReview(r), nil
}

func (f *fakeReviewRepo) Delete(
	ctx context.Context,
	id string,
	userID int,
	anyOwner bool,
) error {
	if err := fakeReviewID(id); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.reviews[id]
	if !ok {
		return repository.ErrNotFound
	}

	if !anyOwner && r.UserID != userID {
		return repository.ErrNotOwner
	}

	delete(f.reviews, id)
	return nil
}

func (f *fakeReviewRepo) AddReply(
	ctx context.Context,
	id string,
	reply model.Reply,
) (model.Review, error) {
	if err := fakeReviewID(id); err != nil {
		return model.Review{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.reviews[id]
	if !ok {
		return model.Review{}, repository.ErrNotFound
	}

	if len(r.Replies) >= repository.MaxRepliesPerReview {
		return model.Review{}, repository.ErrReplyLimit
	}

	reply.ID = bson.NewObjectID().Hex()
	reply.CreatedAt = time.Now().UTC().Truncate(time.Millisecond)

	r.Replies = append(r.Replies, reply)
	r.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)

	f.reviews[id] = cloneReview(r)
	return cloneReview(r), nil
}

func (f *fakeReviewRepo) MarkHelpful(
	ctx context.Context,
	id string,
	userID int,
) (model.Review, error) {
	if err := fakeReviewID(id); err != nil {
		return model.Review{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.reviews[id]
	if !ok {
		return model.Review{}, repository.ErrNotFound
	}

	if r.UserID == userID {
		return model.Review{}, repository.ErrOwnReview
	}

	// Fake menyimpan pengguna yang sudah memberikan helpful
	// melalui map terpisah.
	if f.helpfulBy == nil {
		f.helpfulBy = make(map[string]map[int]bool)
	}
	if f.helpfulBy[id] == nil {
		f.helpfulBy[id] = make(map[int]bool)
	}

	if f.helpfulBy[id][userID] {
		return model.Review{}, repository.ErrAlreadyVoted
	}

	f.helpfulBy[id][userID] = true
	r.HelpfulCount++

	f.reviews[id] = cloneReview(r)
	return cloneReview(r), nil
}

func (f *fakeReviewRepo) Summary(
	ctx context.Context,
	courseID int,
) (model.ReviewSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	summary := model.ReviewSummary{
		CourseID:     courseID,
		Distribution: make(map[string]int),
		TopTags:      []model.TagCount{},
	}

	tagCounts := make(map[string]int)
	totalRating := 0

	for _, r := range f.reviews {
		if r.CourseID != courseID {
			continue
		}

		summary.Count++
		totalRating += r.Rating
		summary.Distribution[strconv.Itoa(r.Rating)]++

		for _, tag := range r.Tags {
			tagCounts[tag]++
		}
	}

	if summary.Count > 0 {
		summary.AverageRating = float64(totalRating) / float64(summary.Count)
	}

	for tag, total := range tagCounts {
		summary.TopTags = append(summary.TopTags, model.TagCount{
			Tag:   tag,
			Total: total,
		})
	}

	sort.Slice(summary.TopTags, func(i, j int) bool {
		if summary.TopTags[i].Total == summary.TopTags[j].Total {
			return summary.TopTags[i].Tag < summary.TopTags[j].Tag
		}
		return summary.TopTags[i].Total > summary.TopTags[j].Total
	})

	if len(summary.TopTags) > 5 {
		summary.TopTags = summary.TopTags[:5]
	}

	return summary, nil
}

var _ repository.CourseRepository = (*fakeCourseRepo)(nil)
var _ repository.ReviewRepository = (*fakeReviewRepo)(nil)
