package service

import (
	"context"
	"testing"

	"latihan-fiber/app/model"
	"latihan-fiber/app/repository"
)

func TestFakeCourseRepoEnrollment(t *testing.T) {
	ctx := context.Background()
	repo := newFakeCourseRepo()

	repo.courses[1] = model.Course{
		ID:      1,
		Code:    "IF101",
		Name:    "Algoritma dan Pemrograman",
		Credits: 3,
		Quota:   2,
	}

	// Menguji penambahan enrollment.
	if err := repo.InsertEnrollment(ctx, 1, 10); err != nil {
		t.Fatalf("InsertEnrollment gagal: %v", err)
	}

	// Menguji status enrollment.
	enrolled, err := repo.IsEnrolled(ctx, 1, 10)
	if err != nil {
		t.Fatalf("IsEnrolled gagal: %v", err)
	}
	if !enrolled {
		t.Fatal("mahasiswa seharusnya terdaftar")
	}

	// Menguji perhitungan total SKS.
	total, err := repo.SumCredits(ctx, 10)
	if err != nil {
		t.Fatalf("SumCredits gagal: %v", err)
	}
	if total != 3 {
		t.Errorf("total SKS = %d, ingin 3", total)
	}

	// Menguji penolakan enrollment duplikat.
	err = repo.InsertEnrollment(ctx, 1, 10)
	if err != repository.ErrDuplicate {
		t.Errorf("error = %v, ingin %v", err, repository.ErrDuplicate)
	}

	// Menguji penghapusan enrollment.
	if err := repo.DeleteEnrollment(ctx, 1, 10); err != nil {
		t.Fatalf("DeleteEnrollment gagal: %v", err)
	}

	enrolled, err = repo.IsEnrolled(ctx, 1, 10)
	if err != nil {
		t.Fatalf("IsEnrolled setelah penghapusan gagal: %v", err)
	}
	if enrolled {
		t.Error("mahasiswa seharusnya tidak lagi terdaftar")
	}
}

func TestFakeReviewRepo(t *testing.T) {
	ctx := context.Background()
	repo := newFakeReviewRepo()

	// Menguji pembuatan ulasan.
	created, err := repo.Create(ctx, model.Review{
		CourseID: 1,
		UserID:   10,
		Username: "mahasiswa",
		Rating:   5,
		Comment:  "Materi mudah dipahami",
		Tags:     []string{"mudah"},
	})
	if err != nil {
		t.Fatalf("Create gagal: %v", err)
	}
	if created.ID == "" {
		t.Fatal("ID ulasan tidak boleh kosong")
	}

	// Menguji pengambilan ulasan berdasarkan ID.
	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID gagal: %v", err)
	}
	if found.UserID != 10 {
		t.Errorf("UserID = %d, ingin 10", found.UserID)
	}

	// Menguji penolakan ulasan duplikat dari pengguna dan kelas yang sama.
	_, err = repo.Create(ctx, model.Review{
		CourseID: 1,
		UserID:   10,
		Rating:   4,
	})
	if err != repository.ErrDuplicate {
		t.Errorf("error duplikat = %v, ingin %v", err, repository.ErrDuplicate)
	}

	// Menguji perubahan ulasan oleh pemiliknya.
	newRating := 4
	newComment := "Materi cukup mudah dipahami"
	newTags := []string{"menarik"}

	updated, err := repo.UpdateOwn(ctx, created.ID, 10, model.UpdateReviewRequest{
		Rating:  &newRating,
		Comment: &newComment,
		Tags:    &newTags,
	})
	if err != nil {
		t.Fatalf("UpdateOwn gagal: %v", err)
	}
	if updated.Rating != 4 {
		t.Errorf("Rating = %d, ingin 4", updated.Rating)
	}

	// Menguji penolakan perubahan oleh bukan pemilik.
	_, err = repo.UpdateOwn(ctx, created.ID, 20, model.UpdateReviewRequest{})
	if err != repository.ErrNotOwner {
		t.Errorf("error pemilik = %v, ingin %v", err, repository.ErrNotOwner)
	}

	// Menguji pemberian helpful.
	_, err = repo.MarkHelpful(ctx, created.ID, 10)
	if err != repository.ErrOwnReview {
		t.Errorf("error ulasan sendiri = %v, ingin %v", err, repository.ErrOwnReview)
	}

	helpful, err := repo.MarkHelpful(ctx, created.ID, 20)
	if err != nil {
		t.Fatalf("MarkHelpful gagal: %v", err)
	}
	if helpful.HelpfulCount != 1 {
		t.Errorf("HelpfulCount = %d, ingin 1", helpful.HelpfulCount)
	}

	// Menguji penolakan helpful berulang.
	_, err = repo.MarkHelpful(ctx, created.ID, 20)
	if err != repository.ErrAlreadyVoted {
		t.Errorf("error helpful berulang = %v, ingin %v", err, repository.ErrAlreadyVoted)
	}

	// Menguji penambahan balasan.
	replied, err := repo.AddReply(ctx, created.ID, model.Reply{
		UserID:   20,
		Username: "mahasiswa2",
		Body:     "Saya setuju",
	})
	if err != nil {
		t.Fatalf("AddReply gagal: %v", err)
	}
	if len(replied.Replies) != 1 {
		t.Errorf("jumlah balasan = %d, ingin 1", len(replied.Replies))
	}

	// Menguji ringkasan ulasan.
	summary, err := repo.Summary(ctx, 1)
	if err != nil {
		t.Fatalf("Summary gagal: %v", err)
	}
	if summary.Count != 1 {
		t.Errorf("jumlah ulasan = %d, ingin 1", summary.Count)
	}
	if summary.AverageRating != 4 {
		t.Errorf("rata-rata rating = %v, ingin 4", summary.AverageRating)
	}
}
