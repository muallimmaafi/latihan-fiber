package helper

import "testing"

type ptrSample struct {
	Username *string `json:"username" validate:"omitnil,min=3,max=30,alphanum"`
}

type plainOmitNil struct {
	Username string `json:"username" validate:"omitnil,min=3"`
}

type plainOmitEmpty struct {
	Username string `json:"username" validate:"omitempty,min=3"`
}

type passSample struct {
	Password string `json:"password" validate:"required,max=72,strongpassword"`
}

type unknownTagSample struct {
	ID string `json:"id" validate:"uuid4"`
}

func TestOmitNilPointer(t *testing.T) {
	empty := ""
	short := "ab"
	ok := "sari01"

	if errs := ValidateStruct(ptrSample{Username: nil}); errs != nil {
		t.Errorf("nil harus dilewati, dapat %v", errs)
	}
	if errs := ValidateStruct(ptrSample{Username: &empty}); errs["username"] == "" {
		t.Error(`pointer ke "" harus DITOLAK`)
	}
	if errs := ValidateStruct(ptrSample{Username: &short}); errs["username"] == "" {
		t.Error("pointer ke string pendek harus DITOLAK")
	}
	if errs := ValidateStruct(ptrSample{Username: &ok}); errs != nil {
		t.Errorf("nilai sah harus lolos, dapat %v", errs)
	}
}

// Tabel A.3 baris pertama: dicatat, bukan dipaksakan, supaya perilaku
// pada versi validator yang kamu pakai terlihat apa adanya.
func TestOmitEmptyVsOmitNilOnPlainString(t *testing.T) {
	t.Logf(`string "" + omitnil   -> %v`, ValidateStruct(plainOmitNil{Username: ""}))
	t.Logf(`string "" + omitempty -> %v`, ValidateStruct(plainOmitEmpty{Username: ""}))
}

func TestStrongPassword(t *testing.T) {
	cases := []struct {
		password string
		wantErr  bool
	}{
		{"Rahasia123!", false},
		{"abc", true},
		{"tanpaangka", true},
		{"password123", true},
	}
	for _, tc := range cases {
		errs := ValidateStruct(passSample{Password: tc.password})
		if tc.wantErr {
			if errs["password"] == "" {
				t.Errorf("%q harus ditolak dengan pesan tidak kosong, dapat %v",
					tc.password, errs)
			}
		} else if errs != nil {
			t.Errorf("%q harus diterima, dapat %v", tc.password, errs)
		}
	}
}

func TestUnknownTagFallbackIsNeverEmpty(t *testing.T) {
	errs := ValidateStruct(unknownTagSample{ID: "x"})
	if errs["id"] == "" {
		t.Error("tag tanpa terjemahan tidak boleh menghasilkan pesan kosong")
	}
}