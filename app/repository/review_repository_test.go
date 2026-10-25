package repository_test

import (
	"context"
	"errors"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
)

// newTestRepo menyambung ke MongoDB dan memakai database terpisah.
// Bila MongoDB tidak menyala, test DILEWATI (terlihat sebagai SKIP pada go
// test -v), bukan lulus diam-diam.
func newTestRepo(t *testing.T) repository.ReviewRepository {
	t.Helper()

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}

	client, err := mongo.Connect(options.Client().
		ApplyURI(uri).SetServerSelectionTimeout(2 * time.Second))
	if err != nil {
		t.Skipf("tidak dapat menyiapkan client MongoDB: %v", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		t.Skipf("MongoDB tidak dapat dihubungi (%v); test integrasi dilewati", err)
	}

	db := client.Database("latihan_fiber_test")
	coll := db.Collection("course_reviews")
	_ = coll.Drop(context.Background())
	t.Cleanup(func() {
		_ = coll.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})

	repo := repository.NewReviewRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repo
}

func newReview(courseID, userID, rating int, tags ...string) model.Review {
	return model.Review{
		CourseID: courseID, UserID: userID, Username: "mhs", Rating: rating,
		Comment: "komentar uji yang cukup panjang", Tags: tags,
	}
}

func mustCreate(t *testing.T, repo repository.ReviewRepository, r model.Review) model.Review {
	t.Helper()
	created, err := repo.Create(context.Background(), r)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

func mustFind(t *testing.T, repo repository.ReviewRepository, id string) model.Review {
	t.Helper()
	found, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	return found
}

// fireAll menjalankan n pekerjaan serentak di depan satu "gerbang" start,
// seperti fungsi fire pada alat uji beban.
func fireAll(n int, job func(i int) error) []error {
	var wg, ready sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		ready.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			errs[i] = job(i)
		}(i)
	}

	ready.Wait()
	close(start)
	wg.Wait()
	return errs
}

func TestCreateDuplicateIsRejectedByUniqueIndex(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	mustCreate(t, repo, newReview(1, 4, 5))

	if _, err := repo.Create(ctx, newReview(1, 4, 3)); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("ulasan kedua mahasiswa yang sama di kelas yang sama harus ErrDuplicate, dapat %v", err)
	}
	if _, err := repo.Create(ctx, newReview(2, 4, 3)); err != nil {
		t.Fatalf("kelas berbeda harus diizinkan: %v", err)
	}
}

func TestOwnershipIsEnforcedInsideTheFilter(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	one := 1
	req := model.UpdateReviewRequest{Rating: &one}

	if _, err := repo.UpdateOwn(ctx, created.ID, 99, req); !errors.Is(err, repository.ErrNotOwner) {
		t.Fatalf("bukan pemilik harus ErrNotOwner, dapat %v", err)
	}
	if got := mustFind(t, repo, created.ID); got.Rating != 5 {
		t.Fatalf("ulasan tidak boleh berubah oleh bukan pemilik, rating = %d", got.Rating)
	}

	updated, err := repo.UpdateOwn(ctx, created.ID, 4, req)
	if err != nil {
		t.Fatalf("pemilik harus boleh mengubah: %v", err)
	}
	if updated.Rating != 1 {
		t.Errorf("rating harus berubah menjadi 1, dapat %d", updated.Rating)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Error("created_at tidak boleh berubah")
	}
	if updated.UpdatedAt.Before(created.CreatedAt) {
		t.Error("updated_at harus tidak lebih lama dari created_at")
	}

	if _, err := repo.UpdateOwn(ctx, "ffffffffffffffffffffffff", 4, req); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("id sah tetapi tidak ada harus ErrNotFound, dapat %v", err)
	}
	if _, err := repo.UpdateOwn(ctx, "bukan-objectid", 4, req); !errors.Is(err, repository.ErrInvalidID) {
		t.Errorf("id rusak harus ErrInvalidID, dapat %v", err)
	}
}

func TestDeleteOwnerAndModerator(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	if err := repo.Delete(ctx, created.ID, 99, false); !errors.Is(err, repository.ErrNotOwner) {
		t.Fatalf("bukan pemilik tanpa hak moderasi harus ErrNotOwner, dapat %v", err)
	}
	if err := repo.Delete(ctx, created.ID, 99, true); err != nil {
		t.Fatalf("moderator harus boleh menghapus: %v", err)
	}
	if err := repo.Delete(ctx, created.ID, 99, true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("menghapus yang sudah terhapus harus ErrNotFound, dapat %v", err)
	}
}

func TestMarkHelpfulOneUserManyTimesConcurrently(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	errs := fireAll(20, func(i int) error {
		_, err := repo.MarkHelpful(ctx, created.ID, 5)
		return err
	})

	ok, voted := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, repository.ErrAlreadyVoted):
			voted++
		}
	}
	if ok != 1 || voted != 19 {
		t.Fatalf("harap 1 berhasil dan 19 ErrAlreadyVoted, dapat %d dan %d", ok, voted)
	}
	if got := mustFind(t, repo, created.ID); got.HelpfulCount != 1 {
		t.Fatalf("helpful_count harus 1, dapat %d", got.HelpfulCount)
	}
}

func TestMarkHelpfulManyUsersConcurrently(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	errs := fireAll(30, func(i int) error {
		_, err := repo.MarkHelpful(ctx, created.ID, 100+i)
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("mahasiswa %d seharusnya berhasil: %v", i, err)
		}
	}
	if got := mustFind(t, repo, created.ID); got.HelpfulCount != 30 {
		t.Fatalf("30 penilai berbeda harus menghasilkan helpful_count 30, dapat %d", got.HelpfulCount)
	}
}

func TestMarkHelpfulRejectsOwnAndMissingReview(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	if _, err := repo.MarkHelpful(ctx, created.ID, 4); !errors.Is(err, repository.ErrOwnReview) {
		t.Errorf("menilai ulasan sendiri harus ErrOwnReview, dapat %v", err)
	}
	if _, err := repo.MarkHelpful(ctx, "ffffffffffffffffffffffff", 5); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("ulasan tidak ada harus ErrNotFound, dapat %v", err)
	}
}

func TestReplyLimitHoldsUnderConcurrency(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	created := mustCreate(t, repo, newReview(1, 4, 5))

	reply := model.Reply{UserID: 5, Username: "mhs", Body: "balasan"}
	for i := 0; i < repository.MaxRepliesPerReview-2; i++ {
		if _, err := repo.AddReply(ctx, created.ID, reply); err != nil {
			t.Fatalf("balasan ke-%d: %v", i+1, err)
		}
	}

	errs := fireAll(10, func(i int) error {
		_, err := repo.AddReply(ctx, created.ID, reply)
		return err
	})
	ok, full := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, repository.ErrReplyLimit):
			full++
		}
	}
	if ok != 2 || full != 8 {
		t.Fatalf("harap tepat 2 berhasil dan 8 ErrReplyLimit, dapat %d dan %d", ok, full)
	}
	if got := mustFind(t, repo, created.ID); len(got.Replies) != repository.MaxRepliesPerReview {
		t.Fatalf("jumlah balasan akhir harus %d, dapat %d",
			repository.MaxRepliesPerReview, len(got.Replies))
	}
	if _, err := repo.AddReply(ctx, created.ID, reply); !errors.Is(err, repository.ErrReplyLimit) {
		t.Fatalf("balasan ke-51 harus ErrReplyLimit, dapat %v", err)
	}
}

func TestFindByCourseCursorHasNoOverlap(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	for u := 1; u <= 5; u++ {
		mustCreate(t, repo, newReview(7, u, 4))
	}

	page1, err := repo.FindByCourse(ctx, model.ReviewQuery{CourseID: 7, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 3 || page1[0].UserID != 5 {
		t.Fatalf("halaman 1 harus 3 ulasan, terbaru lebih dulu; dapat %+v", page1)
	}

	page2, err := repo.FindByCourse(ctx, model.ReviewQuery{
		CourseID: 7, Limit: 3, Before: page1[len(page1)-1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("halaman 2 harus 2 ulasan, dapat %d", len(page2))
	}

	seen := map[string]bool{}
	for _, r := range append(page1, page2...) {
		if seen[r.ID] {
			t.Fatalf("ulasan %s muncul dua kali", r.ID)
		}
		seen[r.ID] = true
	}
}

// Input yang tampak seperti operator MongoDB diperlakukan sebagai teks biasa.
func TestTagFilterTreatsInputAsPlainText(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mustCreate(t, repo, newReview(1, 4, 5, "seru"))

	got, err := repo.FindByCourse(ctx, model.ReviewQuery{
		CourseID: 1, Limit: 10, Tag: `{"$ne":""}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("tag berbentuk operator harus menghasilkan 0 ulasan, dapat %d", len(got))
	}

	got, err = repo.FindByCourse(ctx, model.ReviewQuery{CourseID: 1, Limit: 10, Tag: "seru"})
	if err != nil || len(got) != 1 {
		t.Fatalf("tag biasa harus menemukan 1 ulasan, dapat %d (err %v)", len(got), err)
	}
}

func TestSummaryMatchesTheModuleExample(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	ratings := []int{5, 5, 3, 4, 5, 2, 4, 5}
	tags := [][]string{
		{"dosen-jelas", "praktikal", "seru"},
		{"dosen-jelas", "tugas-banyak"},
		{"tugas-banyak", "deadline-ketat"},
		{"dosen-jelas", "praktikal"},
		{"dosen-jelas", "praktikal", "seru"},
		{"tugas-banyak"},
		nil,
		nil,
	}
	for i, rating := range ratings {
		mustCreate(t, repo, newReview(1, 10+i, rating, tags[i]...))
	}

	s, err := repo.Summary(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 8 {
		t.Errorf("count harus 8, dapat %d", s.Count)
	}
	if math.Abs(s.AverageRating-4.13) > 1e-9 {
		t.Errorf("rata-rata harus 4.13 (33/8 = 4,125), dapat %v", s.AverageRating)
	}
	want := map[string]int{"1": 0, "2": 1, "3": 1, "4": 2, "5": 4}
	for k, v := range want {
		if s.Distribution[k] != v {
			t.Errorf("distribusi bintang %s harus %d, dapat %d", k, v, s.Distribution[k])
		}
	}

	wantTags := []model.TagCount{
		{Tag: "dosen-jelas", Total: 4}, {Tag: "praktikal", Total: 3},
		{Tag: "tugas-banyak", Total: 3}, {Tag: "seru", Total: 2},
		{Tag: "deadline-ketat", Total: 1},
	}
	if len(s.TopTags) != len(wantTags) {
		t.Fatalf("top_tags harus %d item, dapat %+v", len(wantTags), s.TopTags)
	}
	for i := range wantTags {
		if s.TopTags[i] != wantTags[i] {
			t.Errorf("top_tags[%d] harus %+v, dapat %+v", i, wantTags[i], s.TopTags[i])
		}
	}
}

func TestSummaryOfCourseWithoutReviews(t *testing.T) {
	repo := newTestRepo(t)

	s, err := repo.Summary(context.Background(), 999)
	if err != nil {
		t.Fatalf("kelas tanpa ulasan bukan error: %v", err)
	}
	if s.Count != 0 || s.AverageRating != 0 {
		t.Errorf("count dan rata-rata harus 0, dapat %d dan %v", s.Count, s.AverageRating)
	}
	if len(s.Distribution) != 5 {
		t.Errorf("distribusi harus memuat lima bintang, dapat %v", s.Distribution)
	}
	if s.TopTags == nil || len(s.TopTags) != 0 {
		t.Error("top_tags harus array kosong, bukan nil")
	}
}