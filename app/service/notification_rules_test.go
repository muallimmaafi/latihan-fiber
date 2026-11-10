
package service

import (
	"testing"

	"latihan-fiber/app/model"
)

func TestBuildMessage(t *testing.T) {
	event := model.EnrollmentEvent{
		UserID:     1,
		CourseID:   10,
		CourseCode: "IF101",
		CourseName: "Algoritma dan Pemrograman",
		FromCourseCode: "IF100",
		FromCourseName: "Pengantar Informatika",
	}

	user := model.User{
		Username: "mahasiswa",
		Email:    "mahasiswa@example.com",
	}

	tests := []struct {
		name          string
		eventType     string
		wantSubject   string
		wantBody      string
		wantErr       bool
	}{
		{
			name:      "enrollment created",
			eventType: model.EventEnrollmentCreated,
			wantSubject: "KRS: berhasil terdaftar di IF101 Algoritma dan Pemrograman",
			wantBody: "Halo mahasiswa, Anda tercatat sebagai peserta kelas IF101 Algoritma dan Pemrograman.",
		},
		{
			name:      "enrollment dropped",
			eventType: model.EventEnrollmentDropped,
			wantSubject: "KRS: keluar dari IF101 Algoritma dan Pemrograman",
			wantBody: "Halo mahasiswa, Anda sudah tidak terdaftar di kelas IF101 Algoritma dan Pemrograman.",
		},
		{
			name:      "enrollment swapped",
			eventType: model.EventEnrollmentSwapped,
			wantSubject: "KRS: pindah dari IF100 Pengantar Informatika ke IF101 Algoritma dan Pemrograman",
			wantBody: "Halo mahasiswa, Anda telah pindah dari kelas IF100 Pengantar Informatika ke kelas IF101 Algoritma dan Pemrograman.",
		},
		{
			name:      "unknown event",
			eventType: "unknown.event",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildMessage(tt.eventType, event, user)

			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildMessage() seharusnya menghasilkan error")
				}
				return
			}

			if err != nil {
				t.Fatalf("BuildMessage() menghasilkan error: %v", err)
			}

			if got.To != user.Email {
				t.Errorf("To = %q, ingin %q", got.To, user.Email)
			}

			if got.Subject != tt.wantSubject {
				t.Errorf("Subject = %q, ingin %q", got.Subject, tt.wantSubject)
			}

			if got.Body != tt.wantBody {
				t.Errorf("Body = %q, ingin %q", got.Body, tt.wantBody)
			}
		})
	}
}
