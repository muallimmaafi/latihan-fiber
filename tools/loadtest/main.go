// Alat uji beban untuk Modul 9. Bukan bagian aplikasi: tidak ikut di-deploy.
//
// Jalankan dari FOLDER PROYEK (supaya .env terbaca), dengan server menyala:
//
//	go run ./tools/loadtest -mode crowd
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"latihan-fiber/app/model"
	"latihan-fiber/config"
	"latihan-fiber/database"
	"latihan-fiber/helper"
)

const maxCredits = 24

type student struct {
	ID       int
	Username string
	Token    string
}

var client = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 256,
	},
}

// samples menyimpan contoh pesan pertama untuk tiap label, supaya penyebab
// sebuah hasil yang membingungkan (misalnya 404) dapat dibedakan.
var samples sync.Map

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "GAGAL:", msg)
	os.Exit(1)
}

// isInvalid menandai hasil yang tidak mungkin berasal dari logika pendaftaran:
// request ditolak sebelum sampai ke sana (salah alamat, tanpa izin, salah
// bentuk, atau tidak tersambung). 409 sengaja TIDAK termasuk: itu penolakan
// aturan bisnis yang sah.
func isInvalid(label string) bool {
	if label == "ERR timeout" {
		return false // temuan, bukan uji tidak sah
	}
	if strings.HasPrefix(label, "ERR") {
		return true
	}
	for _, p := range []string{"400", "401", "403", "404", "405", "415", "422"} {
		if strings.HasPrefix(label, p) {
			return true
		}
	}
	return false
}

// isServerFailure menandai kegagalan sisi server. Menurut spesifikasi,
// 5xx dan timeout tidak boleh muncul pada uji yang wajar.
func isServerFailure(label string) bool {
	return label == "ERR timeout" || strings.HasPrefix(label, "5")
}

func main() {
	mode := flag.String("mode", "crowd", "crowd | credits | swap | serial")
	baseURL := flag.String("url", "http://localhost:3000/api/v1", "alamat dasar API")
	doReset := flag.Bool("reset", true,
		"kembalikan data akun mhs sebelum uji (sama seperti reset.sql)")
	flag.Parse()

	config.LoadEnv()
	ctx := context.Background()

	secret := config.GetEnv("JWT_SECRET", "")
	if len(secret) < 32 {
		fatal("JWT_SECRET kosong atau terlalu pendek; jalankan dari folder proyek agar .env terbaca")
	}

	pool, err := database.NewPool(ctx)
	if err != nil {
		fatal("database: " + err.Error())
	}
	defer pool.Close()

	// Alat ini mencetak token sendiri karena ia memegang JWT_SECRET.
	// Itulah sebabnya JWT_SECRET tidak boleh bocor: siapa pun yang
	// memegangnya dapat mencetak token atas nama siapa saja.
	jwtManager := helper.NewJWTManager(
		secret, config.GetEnv("JWT_ISSUER", "praktikum-backend"), 15*time.Minute)

	students, err := loadStudents(ctx, pool, jwtManager)
	if err != nil {
		fatal(err.Error())
	}
	need(students, 1)

	ensureServer(*baseURL)
	probeRoute(*mode, *baseURL, students[0].Token)

	if *doReset {
		if err := resetState(ctx, pool); err != nil {
			fatal("reset: " + err.Error())
		}
	}

	switch *mode {
	case "serial":
		runSerial(ctx, pool, *baseURL, students)
	case "crowd":
		runCrowd(ctx, pool, *baseURL, students)
	case "credits":
		runCredits(ctx, pool, *baseURL, students)
	case "swap":
		runSwap(ctx, pool, *baseURL, students)
	default:
		fatal("mode tidak dikenal: " + *mode)
	}
}

// ---------------------------------------------------------------------
// Pemeriksaan sebelum uji
// ---------------------------------------------------------------------

// ensureServer memastikan server benar-benar menjawab sebelum uji dimulai.
// Tanpa ini, seluruh request gagal koneksi dan hasilnya tampak "konsisten"
// hanya karena tidak ada satu pun yang sampai ke database.
func ensureServer(base string) {
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

// probeRoute memastikan alamat yang akan diserang benar-benar dilayani
// aplikasi. Permintaan diarahkan ke id kelas yang tidak ada, sehingga aman:
// tidak mengubah data apa pun. Bila route-nya ada, jawabannya "kelas tidak
// ditemukan"; bila tidak ada, "endpoint tidak ditemukan".
func probeRoute(mode, base, token string) {
	target := enrollURL(base, 999999)
	var body any
	if mode == "swap" {
		target = base + "/enrollments/swap"
		body = map[string]int{"from_course_id": 999998, "to_course_id": 999999}
	}

	r, err := send("POST", target, token, body)
	if err != nil {
		fatal("pemeriksaan route: " + err.Error())
	}
	fmt.Printf("pemeriksaan route POST %s -> %d %s (%s)\n",
		target, r.Status, r.Code, r.Message)

	if r.Status == http.StatusUnauthorized {
		fatal("token alat uji ditolak server; JWT_SECRET atau JWT_ISSUER " +
			"alat uji berbeda dari yang dipakai server")
	}
	if r.Message == "endpoint tidak ditemukan" {
		fatal("route tidak ada di server yang berjalan. Server kemungkinan masih " +
			"memakai kode lama: hentikan, lalu jalankan ulang `go run .`")
	}
}

// ---------------------------------------------------------------------
// fire
// ---------------------------------------------------------------------

// fire menjalankan n pekerjaan secara serentak dan menghitung hasilnya.
//
// Setiap goroutine menunggu di depan "gerbang" start yang sama. Gerbang
// baru dibuka setelah SELURUH goroutine siap, sehingga request benar-benar
// berangkat hampir bersamaan, bukan bergiliran sesuai urutan dibuatnya.
func fire(n int, job func(i int) string) map[string]int {
	var wg, ready sync.WaitGroup
	start := make(chan struct{})
	results := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		ready.Add(1)
		go func(i int, out chan<- string) {
			defer wg.Done()
			ready.Done()
			<-start
			out <- job(i)
		}(i, results)
	}

	ready.Wait() // seluruh goroutine sudah di garis start
	close(start) // buka gerbang
	wg.Wait()    // tunggu SEMUA goroutine selesai
	close(results)

	tally := map[string]int{}
	for r := range results {
		tally[r]++
	}
	return tally
}

type reply struct {
	Status  int
	Code    string
	Message string
}

// send mengirim satu request dan membaca kode serta pesan dari body-nya.
func send(method, url, token string, body any) (reply, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return reply{}, fmt.Errorf("body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return reply{}, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// RequireJSON mewajibkan Content-Type JSON pada POST/PUT/PATCH,
	// termasuk POST tanpa body.
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &e)
	return reply{Status: resp.StatusCode, Code: e.Code, Message: e.Message}, nil
}

// call mengirim satu request dan meringkas hasilnya menjadi satu label.
func call(method, url, token string, body any) string {
	r, err := send(method, url, token, body)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.HasPrefix(msg, "body:"), strings.HasPrefix(msg, "request:"):
			samples.LoadOrStore("ERR request", msg)
			return "ERR request"
		case strings.Contains(msg, "Timeout"), strings.Contains(msg, "deadline"):
			samples.LoadOrStore("ERR timeout", msg)
			return "ERR timeout"
		default:
			samples.LoadOrStore("ERR koneksi", msg)
			return "ERR koneksi"
		}
	}

	if r.Status >= 200 && r.Status < 300 {
		return fmt.Sprintf("%d OK", r.Status)
	}
	if r.Code != "" {
		label := fmt.Sprintf("%d %s", r.Status, r.Code)
		samples.LoadOrStore(label, r.Message)
		return label
	}
	return fmt.Sprint(r.Status)
}

// printTally mencetak hasil dan mengembalikan false bila ada request yang
// tidak sah, sehingga pemanggil menolak menampilkan vonis konsistensi.
func printTally(tally map[string]int, elapsed time.Duration) bool {
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	invalid, serverFail := 0, 0
	fmt.Println("hasil request:")
	for _, k := range keys {
		fmt.Printf("  %-28s %d\n", k, tally[k])
		if isInvalid(k) {
			invalid += tally[k]
		}
		if isServerFailure(k) {
			serverFail += tally[k]
		}
		if isInvalid(k) || isServerFailure(k) {
			if msg, ok := samples.Load(k); ok {
				fmt.Printf("    contoh pesan: %v\n", msg)
			}
		}
	}
	fmt.Printf("waktu total : %dms\n", elapsed.Milliseconds())

	if serverFail > 0 {
		fmt.Printf("TEMUAN: %d request berakhir 5xx atau timeout; pada kode yang benar ini tidak boleh terjadi\n",
			serverFail)
	}
	if invalid > 0 {
		fmt.Printf("PERINGATAN: %d request tidak sampai ke logika pendaftaran; hasil uji ini TIDAK SAH\n",
			invalid)
		return false
	}
	return true
}

func requireValid(ok bool) {
	if !ok {
		fatal("uji tidak sah, vonis konsistensi tidak ditampilkan")
	}
}

// ---------------------------------------------------------------------
// Mode uji
// ---------------------------------------------------------------------

func runSerial(ctx context.Context, pool *pgxpool.Pool, base string, st []student) {
	need(st, 7)
	ids := mustCourseIDs(ctx, pool, "IF399")
	fmt.Println("7 mahasiswa mendaftar ke IF399 satu per satu...")

	invalid := 0
	for i := 0; i < 7; i++ {
		label := call("POST", enrollURL(base, ids["IF399"]), st[i].Token, nil)
		fmt.Printf("  %s -> %s\n", st[i].Username, label)
		if isInvalid(label) {
			invalid++
			if msg, ok := samples.Load(label); ok {
				fmt.Printf("    contoh pesan: %v\n", msg)
			}
		}
	}
	if invalid > 0 {
		fatal(fmt.Sprintf("%d request tidak sampai ke logika pendaftaran; uji tidak sah", invalid))
	}
	checkCourse(ctx, pool, "IF399")
}

func runCrowd(ctx context.Context, pool *pgxpool.Pool, base string, st []student) {
	need(st, 60)
	ids := mustCourseIDs(ctx, pool, "IF399")
	fmt.Println("60 mahasiswa berebut kelas IF399 secara serentak...")

	begin := time.Now()
	tally := fire(60, func(i int) string {
		return call("POST", enrollURL(base, ids["IF399"]), st[i].Token, nil)
	})
	requireValid(printTally(tally, time.Since(begin)))
	checkCourse(ctx, pool, "IF399")
}

func runCredits(ctx context.Context, pool *pgxpool.Pool, base string, st []student) {
	need(st, 10)
	codes := []string{"IF101", "IF102", "IF301", "IF302", "IF303", "IF304",
		"IF305", "IF306", "IF307", "IF308", "IF309", "IF310"}
	ids := mustCourseIDs(ctx, pool, codes...)

	fmt.Printf("10 mahasiswa, masing-masing mendaftar ke %d kelas 3 SKS secara serentak (batas %d SKS)...\n",
		len(codes), maxCredits)

	begin := time.Now()
	tally := fire(10*len(codes), func(i int) string {
		s := st[i/len(codes)]
		return call("POST", enrollURL(base, ids[codes[i%len(codes)]]), s.Token, nil)
	})
	requireValid(printTally(tally, time.Since(begin)))
	checkCredits(ctx, pool, 10)
}

func runSwap(ctx context.Context, pool *pgxpool.Pool, base string, st []student) {
	need(st, 40)
	ids := mustCourseIDs(ctx, pool, "IF101", "IF102")
	a, b := ids["IF101"], ids["IF102"]

	// Kondisi awal yang pasti: reset, lalu 20 mahasiswa di IF101 dan 20
	// mahasiswa lain di IF102.
	if err := resetState(ctx, pool); err != nil {
		fatal("reset: " + err.Error())
	}
	for i := 0; i < 40; i++ {
		course := a
		if i >= 20 {
			course = b
		}
		if _, err := pool.Exec(ctx,
			"INSERT INTO enrollments (course_id, user_id) VALUES ($1, $2)",
			course, st[i].ID); err != nil {
			fatal("menyiapkan data awal: " + err.Error())
		}
	}
	if err := recount(ctx, pool); err != nil {
		fatal("recount: " + err.Error())
	}

	fmt.Println("20 pasang mahasiswa pindah kelas berlawanan arah secara serentak...")
	before := deadlocks(ctx, pool)

	begin := time.Now()
	tally := fire(40, func(i int) string {
		from, to := a, b
		if i >= 20 {
			from, to = b, a
		}
		return call("POST", base+"/enrollments/swap", st[i].Token,
			map[string]int{"from_course_id": from, "to_course_id": to})
	})
	elapsed := time.Since(begin)

	// Statistik PostgreSQL diperbarui dengan sedikit jeda.
	time.Sleep(1500 * time.Millisecond)
	after := deadlocks(ctx, pool)

	requireValid(printTally(tally, elapsed))
	fmt.Printf("deadlock terdeteksi PostgreSQL selama uji: %d\n", after-before)
	checkCourse(ctx, pool, "IF101")
	checkCourse(ctx, pool, "IF102")
}

// ---------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------

func enrollURL(base string, courseID int) string {
	return fmt.Sprintf("%s/courses/%d/enroll", base, courseID)
}

func need(st []student, n int) {
	if len(st) < n {
		fatal(fmt.Sprintf("butuh %d akun mhs, hanya ada %d; jalankan migration 006", n, len(st)))
	}
}

func loadStudents(
	ctx context.Context, pool *pgxpool.Pool, jwtManager *helper.JWTManager,
) ([]student, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, username, role FROM users WHERE username LIKE 'mhs%' ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("mengambil akun mhs: %w", err)
	}
	defer rows.Close()

	var result []student
	for rows.Next() {
		var id int
		var username, role string
		if err := rows.Scan(&id, &username, &role); err != nil {
			return nil, err
		}
		token, err := jwtManager.GenerateAccess(
			model.User{ID: id, Username: username, Role: role})
		if err != nil {
			return nil, err
		}
		result = append(result, student{ID: id, Username: username, Token: token})
	}
	return result, rows.Err()
}

// resetState melakukan hal yang sama seperti reset.sql.
func resetState(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx,
		`DELETE FROM enrollments
		 WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'mhs%')`); err != nil {
		return err
	}
	return recount(ctx, pool)
}

func recount(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx,
		`UPDATE courses c
		 SET enrolled_count = (SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)`)
	return err
}

func mustCourseIDs(ctx context.Context, pool *pgxpool.Pool, codes ...string) map[string]int {
	rows, err := pool.Query(ctx,
		"SELECT code, id FROM courses WHERE code = ANY($1)", codes)
	if err != nil {
		fatal("mengambil id kelas: " + err.Error())
	}
	defer rows.Close()

	ids := map[string]int{}
	for rows.Next() {
		var code string
		var id int
		if err := rows.Scan(&code, &id); err != nil {
			fatal(err.Error())
		}
		ids[code] = id
	}
	if len(ids) != len(codes) {
		fatal("ada kode kelas yang tidak ditemukan; jalankan migration 005")
	}
	return ids
}

func deadlocks(ctx context.Context, pool *pgxpool.Pool) int {
	var n int64
	err := pool.QueryRow(ctx,
		"SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()").Scan(&n)
	if err != nil {
		return 0
	}
	return int(n)
}

func checkCourse(ctx context.Context, pool *pgxpool.Pool, code string) {
	var quota, enrolled, rows int
	err := pool.QueryRow(ctx,
		`SELECT c.quota, c.enrolled_count,
		        (SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)
		 FROM courses c WHERE c.code = $1`, code).Scan(&quota, &enrolled, &rows)
	if err != nil {
		fatal("memeriksa kelas: " + err.Error())
	}
	verdict := "KONSISTEN"
	if enrolled != rows || rows > quota {
		verdict = "TIDAK KONSISTEN"
	}
	fmt.Printf("%s kuota=%d enrolled_count=%d baris enrollments=%d -> %s\n",
		code, quota, enrolled, rows, verdict)
}

func checkCredits(ctx context.Context, pool *pgxpool.Pool, studentCount int) {
	rows, err := pool.Query(ctx,
		`SELECT u.username, SUM(c.credits)::int
		 FROM enrollments e
		 JOIN users u ON u.id = e.user_id
		 JOIN courses c ON c.id = e.course_id
		 WHERE u.username LIKE 'mhs%'
		 GROUP BY u.username
		 HAVING SUM(c.credits) > $1
		 ORDER BY u.username`, maxCredits)
	if err != nil {
		fatal("memeriksa SKS: " + err.Error())
	}
	defer rows.Close()

	violators := 0
	for rows.Next() {
		var username string
		var total int
		if err := rows.Scan(&username, &total); err != nil {
			fatal(err.Error())
		}
		fmt.Printf("  MELEBIHI BATAS: %s mengambil %d SKS\n", username, total)
		violators++
	}

	var mismatched int
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM courses c
		 WHERE c.enrolled_count <> (SELECT COUNT(*) FROM enrollments e WHERE e.course_id = c.id)`,
	).Scan(&mismatched)

	verdict := "KONSISTEN"
	if violators > 0 || mismatched > 0 {
		verdict = "TIDAK KONSISTEN"
	}
	fmt.Printf("seluruh %d mahasiswa <= %d SKS? pelanggar=%d -> %s\n",
		studentCount, maxCredits, violators, verdict)
	fmt.Printf("kelas dengan enrolled_count tidak cocok: %d\n", mismatched)
}