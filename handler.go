package main 

import ( 
    "sort" 
    "strconv" 
    "strings" 
    "time" 
  
    "github.com/gofiber/fiber/v2" 
) 
  
var users []User 
var nextID = 1 
  
func findUserIndex(id int) int { 
    for i := range users { 
        if users[i].ID == id { 
            return i 
        } 
    } 
    return -1 
} 
  
// cocokPencarian memeriksa apakah kata kunci muncul di username atau email. 
func cocokPencarian(u User, kata string) bool { 
    kata = strings.ToLower(kata) 
    return strings.Contains(strings.ToLower(u.Username), kata) || 
        strings.Contains(strings.ToLower(u.Email), kata) 
} 
  
func paramID(c *fiber.Ctx) (int, bool) { 
    id, err := strconv.Atoi(c.Params("id")) 
    if err != nil || id < 1 { 
        return 0, false 
    } 
    return id, true 
}

func listUsers(c *fiber.Ctx) error { 
    q := parseListQuery(c) 
  
    // 1) Saring 
    hasil := []User{} 
    for _, u := range users { 
        if q.IsActive != nil && u.IsActive != *q.IsActive { 
            continue 
        } 
        if q.Search != "" && !cocokPencarian(u, q.Search) { 
            continue 
        } 
        hasil = append(hasil, u) 
    } 
  
    // 2) Urutkan 
    sort.SliceStable(hasil, func(i, j int) bool { 
        var lebihKecil bool 
        switch q.Sort { 
        case "username": 
            lebihKecil = hasil[i].Username < hasil[j].Username 
        case "email": 
            lebihKecil = hasil[i].Email < hasil[j].Email 
        case "created_at": 
            lebihKecil = hasil[i].CreatedAt.Before(hasil[j].CreatedAt) 
        default: 
            lebihKecil = hasil[i].ID < hasil[j].ID 
        } 
        if q.Order == "desc" { 
            return !lebihKecil 
        } 
        return lebihKecil 
    })
	    // 3) Potong sesuai halaman 
    total := len(hasil) 
    totalPages := (total + q.Limit - 1) / q.Limit 
    mulai := (q.Page - 1) * q.Limit 
    if mulai > total { 
        mulai = total 
    } 
    akhir := mulai + q.Limit 
    if akhir > total { 
        akhir = total 
    } 
  
    return okList(c, "daftar user berhasil diambil", hasil[mulai:akhir], &Meta{ 
        Page: q.Page, Limit: q.Limit, Total: total, TotalPages: totalPages, 
    }) 
}

func getUser(c *fiber.Ctx) error {
    id, valid := paramID(c) 
    if !valid { 
        return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif") 
    } 
  
    i := findUserIndex(id) 
    if i == -1 { 
        return fail(c, fiber.StatusNotFound, "user tidak ditemukan") 
    } 
  
    return ok(c, "user ditemukan", users[i]) 
} 
  
func createUser(c *fiber.Ctx) error { 
    var req CreateUserRequest 
    if err := c.BodyParser(&req); err != nil { 
        return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid") 
    } 
  
    errs := map[string]string{} 
    req.Username = strings.TrimSpace(req.Username) 
    req.Email = strings.TrimSpace(req.Email) 
  
    if req.Username == "" { 
        errs["username"] = "wajib diisi" 
    } 
    if !strings.Contains(req.Email, "@") { 
        errs["email"] = "format email tidak valid" 
    } 
    if len(req.Password) < 8 { 
        errs["password"] = "minimal 8 karakter" 
    } 
    for _, u := range users { 
        if strings.EqualFold(u.Username, req.Username) { 
            errs["username"] = "sudah dipakai" 
        } 
    } 
    if len(errs) > 0 { 
        return failValidation(c, errs) 
    } 
  
    baru := User{ 
        ID:        nextID, 
        Username:  req.Username, 
        Email:     req.Email, 
        Password:  req.Password, 
        IsActive:  true, 
        CreatedAt: time.Now(), 
    } 
    users = append(users, baru) 
    nextID++ 
  
    return created(c, "user berhasil dibuat", baru,
	        "/api/v1/users/"+strconv.Itoa(baru.ID)) 
}

// PUT mengganti SELURUH isi. Field yang tidak dikirim dianggap dikosongkan, 
// karena itu semuanya wajib ada. 
func replaceUser(c *fiber.Ctx) error { 
    id, valid := paramID(c) 
    if !valid { 
        return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif") 
    } 
  
    i := findUserIndex(id) 
    if i == -1 { 
        return fail(c, fiber.StatusNotFound, "user tidak ditemukan") 
    } 
  
    var req ReplaceUserRequest 
    if err := c.BodyParser(&req); err != nil { 
        return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid") 
    } 
  
    errs := map[string]string{} 
    if strings.TrimSpace(req.Username) == "" { 
        errs["username"] = "wajib diisi pada PUT" 
    } 
    if !strings.Contains(req.Email, "@") { 
        errs["email"] = "wajib diisi dan berformat email pada PUT" 
    } 
    if len(errs) > 0 { 
        return failValidation(c, errs) 
    } 
  
    users[i].Username = req.Username 
    users[i].Email = req.Email 
    users[i].IsActive = req.IsActive 
  
    return ok(c, "user berhasil diganti seluruhnya", users[i]) 
} 
  
// PATCH hanya mengubah field yang benar-benar dikirim. 
func patchUser(c *fiber.Ctx) error { 
    id, valid := paramID(c) 
    if !valid { 
        return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif") 
    } 
  
    i := findUserIndex(id) 
    if i == -1 { 
        return fail(c, fiber.StatusNotFound, "user tidak ditemukan") 
    } 
  
    var req PatchUserRequest 
    if err := c.BodyParser(&req); err != nil { 
        return fail(c, fiber.StatusBadRequest, "body harus berupa JSON yang valid") 
    } 
  
    if req.Username == nil && req.Email == nil && req.IsActive == nil { 
        return fail(c, fiber.StatusBadRequest, "tidak ada field yang diubah") 
    } 
  
    if req.Username != nil { 
        if strings.TrimSpace(*req.Username) == "" { 
            return failValidation(c, map[string]string{"username": "tidak boleh kosong"}) 
        } 
        users[i].Username = *req.Username 
    } 
    if req.Email != nil { 
        if !strings.Contains(*req.Email, "@") { 
            return failValidation(c, map[string]string{"email": "format email tidak valid"}) 
        } 
        users[i].Email = *req.Email 
    } 
    if req.IsActive != nil { 
        users[i].IsActive = *req.IsActive 
    } 
  
    return ok(c, "user berhasil diperbarui sebagian", users[i]) 
} 
  
func deleteUser(c *fiber.Ctx) error { 
    id, valid := paramID(c) 
    if !valid { 
        return fail(c, fiber.StatusBadRequest, "id harus berupa angka positif") 
    } 
  
    i := findUserIndex(id) 
    if i == -1 { 
        return fail(c, fiber.StatusNotFound, "user tidak ditemukan") 
    } 
  
    users = append(users[:i], users[i+1:]...) 
  
    return noContent(c) // 204: berhasil, dan memang tidak ada yang perlu dikirim 
}