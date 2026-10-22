package database

  
import ( 
    "context" 
    "fmt" 
    "time" 
  
    "github.com/jackc/pgx/v5/pgxpool" 
  
    "latihan-fiber/config" 
) 
  
// NewPool membuat connection pool ke PostgreSQL. 
// 
// Pool, bukan koneksi tunggal: server melayani banyak permintaan sekaligus, 
// sedangkan membuka koneksi baru untuk setiap permintaan sangat mahal. 
// Pool menyediakan sejumlah koneksi siap pakai yang dipinjam lalu dikembalikan. 
func NewPool(ctx context.Context) (*pgxpool.Pool, error) { 
    dsn := fmt.Sprintf( 
        "postgres://%s:%s@%s:%s/%s?sslmode=%s", 
        config.GetEnv("DB_USER", "postgres"), 
        config.GetEnv("DB_PASSWORD", ""), 
        config.GetEnv("DB_HOST", "localhost"), 
        config.GetEnv("DB_PORT", "5432"), 
        config.GetEnv("DB_NAME", "praktikum_backend"), 
        config.GetEnv("DB_SSLMODE", "disable"), 
    ) 
  
    cfg, err := pgxpool.ParseConfig(dsn) 
    if err != nil { 
        return nil, fmt.Errorf("konfigurasi database tidak valid: %w", err) 
    } 
  
    cfg.MaxConns = int32(config.GetEnvInt("DB_MAX_CONNS", 10)) 
    cfg.MinConns = 2 
    cfg.MaxConnLifetime = time.Hour 
    cfg.MaxConnIdleTime = 30 * time.Minute 
  
    pool, err := pgxpool.NewWithConfig(ctx, cfg) 
    if err != nil { 
        return nil, fmt.Errorf("gagal membuat pool: %w", err) 
    } 
  
    // Ping memastikan kredensial benar dan server memang dapat dihubungi. 
    // Tanpa ini, kesalahan baru ketahuan saat permintaan pertama masuk. 
    pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second) 
    defer cancel() 
  
    if err := pool.Ping(pingCtx); err != nil { 
        pool.Close() 
        return nil, fmt.Errorf("gagal terhubung ke database: %w", err)
		// UserRepository adalah KONTRAK penyimpanan data user. 
// Perhatikan: tidak ada satu pun kata "SQL" atau "postgres" di sini. 
type UserRepository interface { 
    FindAll(ctx context.Context, q model.ListQuery) ([]model.User, int, error) 
    FindByID(ctx context.Context, id int) (model.User, error) 
    Create(ctx context.Context, u model.User) (model.User, error) 
    Update(ctx context.Context, u model.User) (model.User, error) 
    Delete(ctx context.Context, id int) error 
} 
  
// kolomUrut adalah daftar putih: pemetaan dari nilai yang boleh dikirim klien 
// ke nama kolom yang sebenarnya. ORDER BY tidak dapat memakai parameter, 
// sehingga nama kolom terpaksa disisipkan sebagai teks. Daftar putih inilah 
// satu-satunya hal yang mencegah SQL injection di titik ini. 
var kolomUrut = map[string]string{ 
    "id":         "id", 
    "username":   "username", 
    "email":      "email", 
    "created_at": "created_at", 
} 
  
type userPostgresRepository struct { 
    pool *pgxpool.Pool 
} 
  
// NewUserRepository mengembalikan interface, bukan struct konkret. 
func NewUserRepository(pool *pgxpool.Pool) UserRepository { 
    return &userPostgresRepository{pool: pool} 
}