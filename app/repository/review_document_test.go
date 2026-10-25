package repository

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNowIsTruncatedToMillisecond(t *testing.T) {
	for i := 0; i < 50; i++ {
		n := now()
		if n.Nanosecond()%int(time.Millisecond) != 0 {
			t.Fatalf("now() harus dibulatkan ke milidetik, dapat %v", n)
		}
		if n.Location() != time.UTC {
			t.Fatalf("now() harus UTC, dapat %v", n.Location())
		}
	}
}

func TestToModel(t *testing.T) {
	oid := bson.NewObjectID()
	rid := bson.NewObjectID()
	at := now()

	doc := reviewDocument{
		ID: oid, CourseID: 1, UserID: 4, Username: "mhs001", Rating: 5,
		Comment: "Materi Go-nya jelas", Tags: nil,
		Replies:      []replyDocument{{ID: rid, UserID: 6, Username: "mhs003", Body: "Setuju", CreatedAt: at}},
		HelpfulCount: 1, HelpfulBy: []int{5}, CreatedAt: at, UpdatedAt: at,
	}
	got := doc.toModel()

	if got.ID != oid.Hex() {
		t.Errorf("ID harus berupa hex string, dapat %q", got.ID)
	}
	if got.Tags == nil || len(got.Tags) != 0 {
		t.Error("tags nil harus menjadi array kosong agar JSON-nya [] bukan null")
	}
	if len(got.Replies) != 1 || got.Replies[0].ID != rid.Hex() {
		t.Errorf("balasan salah diterjemahkan: %+v", got.Replies)
	}
}