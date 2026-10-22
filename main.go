package main 
 
import ( 
    "fmt" 
    "log" 
    "strings" 
    "time" 
  
    "github.com/gofiber/fiber/v2" 
    "github.com/gofiber/fiber/v2/middleware/cors" 
    "github.com/gofiber/fiber/v2/middleware/logger" 
    "github.com/gofiber/fiber/v2/middleware/requestid" 
) 
  
var metodeBerbody = map[string]bool{ 
    fiber.MethodPost:  true, 
    fiber.MethodPut:   true, 
    fiber.MethodPatch: true, 
} 
  
// requireJSON menolak request berisi body yang Content-Type-nya bukan JSON. 
// Status yang tepat untuk kasus ini adalah 415, bukan 400. 
func requireJSON(c *fiber.Ctx) error { 
    if metodeBerbody[c.Method()] { 
        ct := c.Get("Content-Type") 
        if !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) { 
            return fail(c, fiber.StatusUnsupportedMediaType,
				                "Content-Type harus application/json") 
        } 
    } 
    return c.Next() 
} 
  
func main() { 
    app := fiber.New(fiber.Config{ 
        AppName: "Praktikum Backend Lanjut - Pertemuan 2", 
        ErrorHandler: func(c *fiber.Ctx, err error) error { 
            status := fiber.StatusInternalServerError 
            pesan := "terjadi kesalahan pada server" 
            if e, ok := err.(*fiber.Error); ok { 
                status = e.Code 
                pesan = e.Message 
            } 
            return fail(c, status, pesan) 
        }, 
    }) 
  
    // Middleware global — urutan pemasangan menentukan urutan eksekusi 
    app.Use(requestid.New()) 
    app.Use(logger.New(logger.Config{ 
        Format: "[${time}] ${locals:requestid} ${method} ${path} ${status} ${latency}\n", 
    })) 
    app.Use(cors.New()) 
  
    app.Get("/", func(c *fiber.Ctx) error { 
        return c.SendString("Hello, World!") 
    }) 
  
    api := app.Group("/api/v1") 
  
    api.Get("/health", func(c *fiber.Ctx) error { 
        return ok(c, "server berjalan", fiber.Map{"timestamp": time.Now()}) 
    }) 
  
    // requireJSON dipasang khusus pada grup ini, bukan global 
    u := api.Group("/users", requireJSON) 
    u.Get("/", listUsers) 
    u.Get("/:id", getUser) 
    u.Post("/", createUser) 
    u.Put("/:id", replaceUser) 
    u.Patch("/:id", patchUser) 
    u.Delete("/:id", deleteUser) 
  
    // Endpoint yang tidak dikenal 
    app.Use(func(c *fiber.Ctx) error { 
        return fail(c, fiber.StatusNotFound, "endpoint tidak ditemukan") 
    }) 
  
    fmt.Println("Server berjalan di http://localhost:3000") 
    log.Fatal(app.Listen(":3000")) 
}