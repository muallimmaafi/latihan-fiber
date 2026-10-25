package helper

import (
	"testing"

	"latihan-fiber/app/model"
)

func TestReviewRequestMessages(t *testing.T) {
	req := model.CreateReviewRequest{
		Rating:  9,
		Comment: "pendek",
		Tags:    []string{"aa", "bb", "cc", "dd", "ee", "ff"},
	}
	errs := ValidateStruct(req)

	want := map[string]string{
		"rating":  "nilai maksimal 5",
		"comment": "minimal 10 karakter",
		"tags":    "maksimal 5 item",
	}
	for field, msg := range want {
		if errs[field] != msg {
			t.Errorf("%s: harap %q, dapat %q (semua: %v)", field, msg, errs[field], errs)
		}
	}
}

func TestUpdateReviewRequestLimits(t *testing.T) {
	if errs := ValidateStruct(model.UpdateReviewRequest{}); errs != nil {
		t.Errorf("semua field nil harus dilewati, dapat %v", errs)
	}

	six := []string{"aa", "bb", "cc", "dd", "ee", "ff"}
	if errs := ValidateStruct(model.UpdateReviewRequest{Tags: &six}); errs["tags"] != "maksimal 5 item" {
		t.Errorf("pointer ke slice 6 item harus ditolak, dapat %v", errs)
	}

	short := "pendek"
	if errs := ValidateStruct(model.UpdateReviewRequest{Comment: &short}); errs["comment"] != "minimal 10 karakter" {
		t.Errorf("komentar pendek harus ditolak, dapat %v", errs)
	}
}