package main 
  
import ( 
    "strconv" 
    "strings" 
  
    "github.com/gofiber/fiber/v2" 
) 
  
func ok(c *fiber.Ctx, message string, data any) error { 
    return c.Status(fiber.StatusOK).JSON(WebResponse{ 
        Success: true, Message: message, Data: data, 
    }) 
} 
  
func okList(c *fiber.Ctx, message string, data any, meta *Meta) error { 
    return c.Status(fiber.StatusOK).JSON(WebResponse{ 
        Success: true, Message: message, Data: data, Meta: meta, 
    }) 
} 
  
func created(c *fiber.Ctx, message string, data any, location string) error { 
    c.Set("Location", location) // memberi tahu klien di mana sumber daya baru berada 
    return c.Status(fiber.StatusCreated).JSON(WebResponse{ 
        Success: true, Message: message, Data: data, 
    }) 
} 
  
func noContent(c *fiber.Ctx) error { 
    return c.SendStatus(fiber.StatusNoContent) // 204: berhasil, tanpa body 
} 
  
func fail(c *fiber.Ctx, status int, message string) error { 
    return c.Status(status).JSON(WebResponse{Success: false, Message: message}) 
} 
  
func failValidation(c *fiber.Ctx, errs map[string]string) error { 
    return c.Status(fiber.StatusUnprocessableEntity).JSON(WebResponse{ 
        Success: false, Message: "validasi gagal", Errors: errs, 
    }) 
} 
  
// Daftar putih field yang boleh dipakai untuk mengurutkan. 
var allowedSort = map[string]bool{ 
    "id": true, "username": true, "email": true, "created_at": true, 
} 
  
// parseListQuery membaca query string dan memberi nilai bawaan yang aman. 
// Aturan pentingnya: masukan dari klien tidak pernah dipercaya begitu saja. 
func parseListQuery(c *fiber.Ctx) ListQuery { 
    q := ListQuery{ 
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
    if q.Limit > 100 { // batas atas wajib ada 
        q.Limit = 100 
    } 
    if !allowedSort[q.Sort] { // daftar putih, bukan daftar hitam 
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