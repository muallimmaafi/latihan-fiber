package service

import (
	"reflect"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{"Tugas-Banyak", " dosenjelas ", "tugas-banyak", "", "   "})
	want := []string{"tugas-banyak", "dosenjelas"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("harap %v, dapat %v", want, got)
	}
}

func TestNormalizeTagsNilGivesEmptySlice(t *testing.T) {
	got := NormalizeTags(nil)
	if got == nil || len(got) != 0 {
		t.Errorf("nil harus menjadi slice kosong (bukan nil), dapat %#v", got)
	}
}