package helper

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
)

func negotiationApp() *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var appErr *AppError
			if errors.As(err, &appErr) {
				return c.Status(appErr.Status).SendString(appErr.Code)
			}
			return c.Status(500).SendString("x")
		},
	})
	app.Get("/", func(c *fiber.Ctx) error {
		format, err := Negotiate(c, FormatJSON, FormatCSV)
		if err != nil {
			return err
		}
		return c.SendString(format)
	})
	return app
}

func TestNegotiate(t *testing.T) {
	cases := []struct {
		accept     string
		wantStatus int
		wantBody   string
	}{
		{"", 200, FormatJSON},
		{"*/*", 200, FormatJSON},
		{"text/csv", 200, FormatCSV},
		{"application/xml", 406, "NOT_ACCEPTABLE"},
	}
	app := negotiationApp()
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		if tc.accept != "" {
			req.Header.Set("Accept", tc.accept)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("accept=%q: %v", tc.accept, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != tc.wantStatus || string(body) != tc.wantBody {
			t.Errorf("accept=%q: harap %d %q, dapat %d %q",
				tc.accept, tc.wantStatus, tc.wantBody, resp.StatusCode, body)
		}
	}
}

func TestWriteUsersCSVContainsData(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return WriteUsersCSV(c, []model.User{
			{ID: 1, Username: "sari", Email: "sari@unair.ac.id", Role: "user",
				IsActive: true,
				CreatedAt: time.Date(2026, 9, 13, 14, 12, 40, 0, time.UTC)},
			{ID: 2, Username: `a,"b"`, Email: "x@unair.ac.id", Role: "user"},
		})
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		"id,username,email,role,is_active,created_at",
		"1,sari,sari@unair.ac.id,user,true,2026-09-13T14:12:40Z",
		`"a,""b"""`, // koma dan kutip ditangani encoding/csv sesuai RFC 4180
	} {
		if !strings.Contains(text, want) {
			t.Errorf("isi CSV tidak memuat %q, isinya:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("Content-Type salah: %s", resp.Header.Get("Content-Type"))
	}
}