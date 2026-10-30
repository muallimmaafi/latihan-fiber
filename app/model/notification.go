
package model

import (
	"encoding/json"
	"time"
)

const (
	EventEnrollmentCreated = "enrollment.created"
	EventEnrollmentDropped = "enrollment.dropped"
	EventEnrollmentSwapped = "enrollment.swapped"
)

const (
	TaskSendNotification = "notification:send"
	QueueNotifications   = "notifications"
)

type EnrollmentEvent struct {
	UserID         int    `json:"user_id"`
	CourseID       int    `json:"course_id"`
	CourseCode     string `json:"course_code"`
	CourseName     string `json:"course_name"`
	FromCourseCode string `json:"from_course_code,omitempty"`
	FromCourseName string `json:"from_course_name,omitempty"`
}

type OutboxEvent struct {
	ID        int64
	EventType string
	Payload   []byte
	CreatedAt time.Time
}

type NotificationJob struct {
	OutboxID  int64           `json:"outbox_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Notification struct {
	ID       int64     `json:"id"`
	OutboxID int64     `json:"outbox_id"`
	UserID   int       `json:"user_id"`
	Channel  string    `json:"channel"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body"`
	SentAt   time.Time `json:"sent_at"`
}
