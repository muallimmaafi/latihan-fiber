package helper

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
)

// RequestContext memberi timeout untuk setiap operasi database.
func RequestContext(c *fiber.Ctx) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.UserContext(), 5*time.Second)
}

// ParamID membaca parameter :id dari jalur dan memastikan bentuknya benar.
func ParamID(c *fiber.Ctx) (int, bool) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}

var allowedSort = map[string]bool{
	"id": true, "username": true, "email": true, "created_at": true,
}

// ParseListQuery membaca query string dan memberi nilai bawaan yang aman.
func ParseListQuery(c *fiber.Ctx) model.ListQuery {
	q := model.ListQuery{
		Page:   c.QueryInt("page", 1),
		Limit:  c.QueryInt("limit", 10),
		Search: strings.TrimSpace(c.Query("search")),
		Sort:   c.Query("sort", "id"),
		Order:  strings.ToLower(c.Query("order", "asc")),
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 10
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	if !allowedSort[q.Sort] {
		q.Sort = "id"
	}
	if q.Order != "desc" {
		q.Order = "asc"
	}
	if raw := c.Query("is_active"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			q.IsActive = &v
		}
	}
	return q
}

// ParseCursorQuery membaca query string daftar berbasis cursor.
// Masukan yang salah bentuk dijawab 400, tidak diam-diam diganti nilai bawaan.
func ParseCursorQuery(c *fiber.Ctx) (model.CursorQuery, error) {
	q := model.CursorQuery{
		Limit:  c.QueryInt("limit", 10),
		Search: strings.TrimSpace(c.Query("search")),
	}
	if q.Limit < 1 {
		q.Limit = 10
	}
	if q.Limit > 100 { // batas atas wajib ada
		q.Limit = 100
	}

	if raw := c.Query("is_active"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return q, BadRequest("is_active harus true atau false")
		}
		q.IsActive = &v
	}

	if raw := c.Query("cursor"); raw != "" {
		cur, err := DecodeCursor(raw)
		if err != nil {
			return q, BadRequest("cursor tidak valid")
		}
		q.After = &cur
	}
	return q, nil
}