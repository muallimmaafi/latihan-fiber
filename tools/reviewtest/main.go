// Alat uji endpoint ulasan untuk Modul 10. Bukan bagian aplikasi: tidak ikut
// di-deploy.
//
// Jalankan dari FOLDER PROYEK (supaya .env terbaca), dengan server menyala:
//
//	go run ./tools/reviewtest
//	go run ./tools/reviewtest -token mhs001   (hanya mencetak token)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"latihan-fiber/app/model"
	"latihan-fiber/config"
	"latihan-fiber/database"
	"latihan-fiber/helper"
)

type actor struct {
	ID       int
	Username string
	Token    string
}

type reply struct {
	Status int
	Header http.Header
	Body   map[string]any
}

var (
	client = &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        256,
			MaxIdleConnsPerHost: 256,
		},
	}
	base           string
	passed, failed int
)

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "GAGAL:", msg)
	os.Exit(1)
}

// ---------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------

// send mengirim satu request. Status -1 berarti tidak tersambung.
func send(method, path, token, body string) reply {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		return reply{Status: -1}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// RequireJSON mewajibkan Content-Type JSON pada POST/PUT/PATCH,
	// termasuk POST tanpa body.
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return reply{Status: -1}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	return reply{Status: resp.StatusCode, Header: resp.Header, Body: parsed}
}

// dig menelusuri JSON bersarang; mengembalikan nil bila jalurnya tidak ada.
func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[p]
	}
	return cur
}

func str(v any) string  { s, _ := v.(string); return s }
func num(v any) int     { f, _ := v.(float64); return int(f) }
func flt(v any) float64 { f, _ := v.(float64); return f }

// fireAll menjalankan n pekerjaan serentak di depan satu "gerbang" start,
// seperti fungsi fire pada alat uji beban Modul 9.
func fireAll(n int, job func(i int) reply) []reply {
	var wg, ready sync.WaitGroup
	start := make(chan struct{})
	out := make([]reply, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		ready.Add(1)
		go func(i int) {
			defer wg.Done()
			ready.Done()
			<-start
			out[i] = job(i)
		}(i)
	}

	ready.Wait()
	close(start)
	wg.Wait()
	return out
}

func tallyOf(rs []reply) map[string]int {
	t := map[string]int{}
	for _, r := range rs {
		label := fmt.Sprint(r.Status)
		if c := str(r.Body["code"]); c != "" {
			label += " " + c
		}
		t[label]++
	}
	return t
}

func tallyText(t map[string]int) string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s x%d", k, t[k]))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------
// Pelaporan
// ---------------------------------------------------------------------

func section(title string) { fmt.Printf("\n== %s ==\n", title) }

func check(label string, ok bool, detail string) {
	if ok {
		passed++
		fmt.Printf("  [LULUS]  %s\n", label)
		return
	}
	failed++
	fmt.Printf("  [GAGAL]  %s -> %s\n", label, detail)
}

// expect memeriksa status HTTP dan (bila diberikan) kode error.
func expect(label string, r reply, status int, code string) bool {
	got := fmt.Sprint(r.Status)
	if c := str(r.Body["code"]); c != "" {
		got += " " + c
	}
	want := fmt.Sprint(status)
	if code != "" {
		want += " " + code
	}
	ok := r.Status == status && (code == "" || str(r.Body["code"]) == code)
	check(label+" ("+want+")", ok, "dapat "+got)
	return ok
}

// ---------------------------------------------------------------------
// Persiapan
// ---------------------------------------------------------------------

func loadActors(
	ctx context.Context, pool *pgxpool.Pool, jwtManager *helper.JWTManager,
) (map[string]actor, []actor, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, username, role FROM users
		 WHERE username LIKE 'mhs%' OR username = 'admin'
		 ORDER BY username`)
	if err != nil {
		return nil, nil, fmt.Errorf("mengambil akun uji: %w", err)
	}
	defer rows.Close()

	byName := map[string]actor{}
	var students []actor
	for rows.Next() {
		var id int
		var username, role string
		if err := rows.Scan(&id, &username, &role); err != nil {
			return nil, nil, err
		}
		// Alat ini mencetak token sendiri karena ia memegang JWT_SECRET.
		// Itulah sebabnya JWT_SECRET tidak boleh bocor.
		token, err := jwtManager.GenerateAccess(
			model.User{ID: id, Username: username, Role: role})
		if err != nil {
			return nil, nil, err
		}
		a := actor{ID: id, Username: username, Token: token}
		byName[username] = a
		if strings.HasPrefix(username, "mhs") {
			students = append(students, a)
		}
	}
	return byName, students, rows.Err()
}

// resetState mengembalikan data uji ke keadaan awal yang pasti: hanya
// mhs001 dan mhs002 yang terdaftar di IF101, dan tidak ada ulasan buatan
// akun mhs di MongoDB.
func resetState(
	ctx context.Context, pool *pgxpool.Pool, mdb *mongo.Database, students []actor,
) error {
	if _, err := pool.Exec(ctx,
		`DELETE FROM enrollments
		 WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'mhs%')`); err != nil {
		return fmt.Errorf("menghapus pendaftaran uji: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO enrollments (course_id, user_id)
		 SELECT c.id, u.id FROM courses c, users u
		 WHERE c.code = 'IF101' AND u.username IN ('mhs001', 'mhs002')`); err != nil {
		return fmt.Errorf("mendaftarkan mhs001 dan mhs002: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE courses c
		 SET enrolled_count = (SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)`); err != nil {
		return fmt.Errorf("menghitung ulang peserta: %w", err)
	}

	ids := make([]int, 0, len(students))
	for _, s := range students {
		ids = append(ids, s.ID)
	}
	if _, err := mdb.Collection("course_reviews").DeleteMany(ctx,
		bson.M{"user_id": bson.M{"$in": ids}}); err != nil {
		return fmt.Errorf("menghapus ulasan uji di MongoDB: %w", err)
	}
	return nil
}

func ensureServer() {
	resp, err := client.Get(base + "/health")
	if err != nil {
		fatal("server tidak menjawab di " + base +
			"; jalankan `go run .` di terminal lain lebih dulu")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Sprintf("health check mengembalikan %d, bukan 200", resp.StatusCode))
	}
}

func main() {
	baseURL := flag.String("url", "http://localhost:3000/api/v1", "alamat dasar API")
	tokenFor := flag.String("token", "", "cetak token akun ini lalu keluar")
	flag.Parse()
	base = *baseURL

	config.LoadEnv()
	ctx := context.Background()

	secret := config.GetEnv("JWT_SECRET", "")
	if len(secret) < 32 {
		fatal("JWT_SECRET kosong atau terlalu pendek; jalankan dari folder proyek agar .env terbaca")
	}

	pool, err := database.NewPool(ctx)
	if err != nil {
		fatal("PostgreSQL: " + err.Error())
	}
	defer pool.Close()

	jwtManager := helper.NewJWTManager(
		secret, config.GetEnv("JWT_ISSUER", "praktikum-backend"), 15*time.Minute)

	byName, students, err := loadActors(ctx, pool, jwtManager)
	if err != nil {
		fatal(err.Error())
	}

	if *tokenFor != "" {
		a, ok := byName[*tokenFor]
		if !ok {
			fatal("akun tidak ditemukan: " + *tokenFor)
		}
		fmt.Println(a.Token)
		return
	}

	admin, ok := byName["admin"]
	if !ok {
		fatal("akun admin tidak ditemukan; alat ini membutuhkannya untuk uji hapus")
	}
	if len(students) < 40 {
		fatal(fmt.Sprintf("butuh minimal 40 akun mhs, hanya ada %d; jalankan migration 006", len(students)))
	}

	mongoClient, err := database.NewMongoClient(ctx)
	if err != nil {
		fatal("MongoDB: " + err.Error())
	}
	defer func() { _ = mongoClient.Disconnect(context.Background()) }()
	mdb := mongoClient.Database(config.GetEnv("MONGO_DATABASE", "latihan_fiber"))

	var courseID int
	if err := pool.QueryRow(ctx,
		"SELECT id FROM courses WHERE code = 'IF101'").Scan(&courseID); err != nil {
		fatal("kelas IF101 tidak ditemukan; jalankan migration 005")
	}

	ensureServer()

	// Pemeriksaan awal: alamat yang akan diuji harus dilayani aplikasi.
	probe := send("GET", fmt.Sprintf("/courses/%d/reviews", courseID), students[0].Token, "")
	if probe.Status != http.StatusOK {
		fatal(fmt.Sprintf("GET /courses/%d/reviews menghasilkan %d (%s). "+
			"401: JWT_SECRET alat ini berbeda dari server. "+
			"404 'endpoint tidak ditemukan': server masih kode lama, jalankan ulang `go run .`.",
			courseID, probe.Status, str(probe.Body["message"])))
	}

	if err := resetState(ctx, pool, mdb, students); err != nil {
		fatal(err.Error())
	}

	runScenarios(courseID, students, admin)

	fmt.Printf("\nRINGKASAN: %d lulus, %d gagal\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------
// Skenario
// ---------------------------------------------------------------------

func runScenarios(cid int, st []actor, admin actor) {
	m1, m2, m3 := st[0], st[1], st[2]
	reviews := fmt.Sprintf("/courses/%d/reviews", cid)
	valid := `{"rating":4,"comment":"Materi Go-nya jelas, tugasnya cukup banyak","tags":["tugas-banyak","dosen-jelas"]}`

	// ---------------------------------------------------------------
	section("1. Menulis ulasan")

	r := send("POST", reviews, m3.Token, valid)
	expect("mhs003 (bukan peserta) menulis ulasan", r, 403, "NOT_ENROLLED")

	normalize := `{"rating":4,"comment":"Materi Go-nya jelas, tugasnya cukup banyak","tags":["Tugas-Banyak"," dosenjelas ","tugas-banyak"]}`
	r = send("POST", reviews, m1.Token, normalize)
	expect("mhs001 menulis ulasan", r, 201, "")
	id1 := str(dig(r.Body, "data", "id"))
	if id1 == "" {
		fatal("ulasan pertama gagal dibuat, uji tidak dapat dilanjutkan")
	}
	check("tag dinormalisasi menjadi [tugas-banyak dosenjelas]",
		reflect.DeepEqual(dig(r.Body, "data", "tags"), []any{"tugas-banyak", "dosenjelas"}),
		fmt.Sprint(dig(r.Body, "data", "tags")))
	check("header Location berisi alamat ulasan",
		r.Header.Get("Location") == "/api/v1/reviews/"+id1, r.Header.Get("Location"))

	r = send("POST", reviews, m1.Token, valid)
	expect("mhs001 menulis ulasan kedua di kelas yang sama", r, 409, "ALREADY_REVIEWED")

	r = send("POST", reviews, m2.Token,
		`{"rating":9,"comment":"pendek","tags":["aa","bb","cc","dd","ee","ff"]}`)
	expect("rating 9, komentar pendek, 6 tag", r, 422, "VALIDATION_ERROR")
	check("rincian field rating: nilai maksimal 5",
		str(dig(r.Body, "fields", "rating")) == "nilai maksimal 5",
		str(dig(r.Body, "fields", "rating")))
	check("rincian field comment: minimal 10 karakter",
		str(dig(r.Body, "fields", "comment")) == "minimal 10 karakter",
		str(dig(r.Body, "fields", "comment")))
	check("rincian field tags: maksimal 5 item",
		str(dig(r.Body, "fields", "tags")) == "maksimal 5 item",
		str(dig(r.Body, "fields", "tags")))

	r = send("POST", "/courses/99999/reviews", m1.Token, valid)
	expect("ulasan untuk kelas 99999", r, 404, "NOT_FOUND")

	r = send("GET", "/reviews/bukan-objectid", m1.Token, "")
	expect("GET /reviews/bukan-objectid", r, 400, "BAD_REQUEST")

	// ---------------------------------------------------------------
	section("2. Mengubah ulasan")

	r = send("PATCH", "/reviews/"+id1, m2.Token, `{"rating":1}`)
	expect("mhs002 mengubah ulasan mhs001", r, 403, "FORBIDDEN")

	before := send("GET", "/reviews/"+id1, m1.Token, "")
	time.Sleep(30 * time.Millisecond)
	r = send("PATCH", "/reviews/"+id1, m1.Token, `{"rating":5}`)
	expect("mhs001 mengubah ulasannya sendiri", r, 200, "")
	check("rating berubah menjadi 5",
		num(dig(r.Body, "data", "rating")) == 5, fmt.Sprint(dig(r.Body, "data", "rating")))
	check("created_at tetap",
		str(dig(r.Body, "data", "created_at")) == str(dig(before.Body, "data", "created_at")),
		str(dig(r.Body, "data", "created_at")))
	check("updated_at berubah",
		str(dig(r.Body, "data", "updated_at")) != str(dig(before.Body, "data", "updated_at")),
		str(dig(r.Body, "data", "updated_at")))

	r = send("PATCH", "/reviews/"+id1, m1.Token, `{}`)
	expect("PATCH tanpa field apa pun", r, 400, "BAD_REQUEST")

	r = send("PATCH", "/reviews/"+id1, m1.Token, `{"comment":"pendek"}`)
	expect("PATCH komentar terlalu pendek", r, 422, "VALIDATION_ERROR")

	// ---------------------------------------------------------------
	section("3. Menilai ulasan membantu")

	r = send("POST", "/reviews/"+id1+"/helpful", m1.Token, "")
	expect("mhs001 menilai ulasannya sendiri", r, 409, "OWN_REVIEW")

	r = send("POST", "/reviews/"+id1+"/helpful", m2.Token, "")
	expect("mhs002 menilai membantu", r, 200, "")
	check("helpful_count menjadi 1",
		num(dig(r.Body, "data", "helpful_count")) == 1,
		fmt.Sprint(dig(r.Body, "data", "helpful_count")))

	r = send("POST", "/reviews/"+id1+"/helpful", m2.Token, "")
	expect("mhs002 mengulangi penilaian", r, 409, "ALREADY_VOTED")

	r = send("POST", "/reviews/ffffffffffffffffffffffff/helpful", m2.Token, "")
	expect("menilai ulasan yang tidak ada", r, 404, "NOT_FOUND")

	// ---------------------------------------------------------------
	section("4. Uji serentak")

	r = send("POST", reviews, m2.Token, valid)
	expect("mhs002 menulis ulasan (bahan uji hapus)", r, 201, "")
	id2 := str(dig(r.Body, "data", "id"))
	if id2 == "" {
		fatal("ulasan mhs002 gagal dibuat, uji tidak dapat dilanjutkan")
	}

	helpfulOf := func(id string) int {
		return num(dig(send("GET", "/reviews/"+id, m1.Token, "").Body, "data", "helpful_count"))
	}

	t := tallyOf(fireAll(30, func(i int) reply {
		return send("POST", "/reviews/"+id2+"/helpful", st[3+i].Token, "")
	}))
	fmt.Printf("    hasil: %s\n", tallyText(t))
	check("30 mahasiswa berbeda menilai serentak: semuanya 200", t["200"] == 30, tallyText(t))
	count2 := helpfulOf(id2)
	check("helpful_count tepat 30", count2 == 30, fmt.Sprint(count2))

	beforeCount := helpfulOf(id1)
	t = tallyOf(fireAll(20, func(i int) reply {
		return send("POST", "/reviews/"+id1+"/helpful", st[33].Token, "")
	}))
	fmt.Printf("    hasil: %s\n", tallyText(t))
	check("1 mahasiswa menilai 20 kali serentak: 1 berhasil, 19 ditolak",
		t["200"] == 1 && t["409 ALREADY_VOTED"] == 19, tallyText(t))
	afterCount := helpfulOf(id1)
	check("helpful_count bertambah tepat 1", afterCount-beforeCount == 1,
		fmt.Sprintf("sebelum %d, sesudah %d", beforeCount, afterCount))

	// ---------------------------------------------------------------
	section("5. Balasan dan batasnya")

	replyBody := `{"body":"balasan uji"}`
	ok48 := 0
	for i := 0; i < 48; i++ {
		if send("POST", "/reviews/"+id1+"/replies", m3.Token, replyBody).Status == 201 {
			ok48++
		}
	}
	check("48 balasan beruntun diterima", ok48 == 48, fmt.Sprint(ok48))

	t = tallyOf(fireAll(10, func(i int) reply {
		return send("POST", "/reviews/"+id1+"/replies", st[3+i].Token, replyBody)
	}))
	fmt.Printf("    hasil: %s\n", tallyText(t))
	check("10 balasan serentak: tepat 2 masuk, 8 ditolak",
		t["201"] == 2 && t["409 REPLY_LIMIT"] == 8, tallyText(t))

	final := send("GET", "/reviews/"+id1, m1.Token, "")
	replies, _ := dig(final.Body, "data", "replies").([]any)
	check("jumlah balasan akhir tepat 50", len(replies) == 50, fmt.Sprint(len(replies)))

	r = send("POST", "/reviews/"+id1+"/replies", m3.Token, replyBody)
	expect("balasan ke-51", r, 409, "REPLY_LIMIT")

	// ---------------------------------------------------------------
	section("6. Menghapus ulasan")

	r = send("DELETE", "/reviews/"+id2, m3.Token, "")
	expect("mhs003 menghapus ulasan mhs002", r, 403, "FORBIDDEN")

	r = send("DELETE", "/reviews/"+id2, admin.Token, "")
	expect("admin menghapus ulasan mhs002", r, 204, "")

	r = send("DELETE", "/reviews/"+id2, admin.Token, "")
	expect("menghapus ulasan yang sudah terhapus", r, 404, "NOT_FOUND")

	// ---------------------------------------------------------------
	section("7. NoSQL injection")

	r = send("POST", reviews, m1.Token, `{"rating":5,"comment":{"$ne":""}}`)
	expect("body berisi operator pada comment (POST)", r, 400, "BAD_REQUEST")

	r = send("PATCH", "/reviews/"+id1, m1.Token, `{"comment":{"$ne":""}}`)
	expect("body berisi operator pada comment (PATCH)", r, 400, "BAD_REQUEST")

	r = send("GET", reviews+"?tag="+url.QueryEscape(`{"$ne":""}`), m1.Token, "")
	expect("?tag berisi operator", r, 200, "")
	data, _ := dig(r.Body, "data").([]any)
	check("operator di ?tag diperlakukan sebagai teks: 0 hasil",
		len(data) == 0, fmt.Sprint(len(data)))

	// Kontrol positif: tanpa ini, "0 hasil" bisa berarti pencariannya rusak.
	r = send("GET", reviews+"?tag=tugas-banyak", m1.Token, "")
	expect("?tag biasa", r, 200, "")
	data, _ = dig(r.Body, "data").([]any)
	check("kontrol positif: tag biasa menemukan 1 ulasan",
		len(data) == 1, fmt.Sprint(len(data)))

	// ---------------------------------------------------------------
	section("8. Ringkasan dan autentikasi")

	r = send("GET", reviews+"/summary", m1.Token, "")
	expect("ringkasan kelas", r, 200, "")
	check("count 1 (ulasan mhs002 sudah dihapus)",
		num(dig(r.Body, "data", "count")) == 1, fmt.Sprint(dig(r.Body, "data", "count")))
	check("rata-rata rating 5",
		flt(dig(r.Body, "data", "average_rating")) == 5,
		fmt.Sprint(dig(r.Body, "data", "average_rating")))

	r = send("GET", "/reviews/"+id1, "", "")
	expect("tanpa header Authorization", r, 401, "UNAUTHORIZED")
}