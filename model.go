package main 
  
import "time" 
  
type User struct { 
    ID        int       `json:"id"` 
    Username  string    `json:"username"` 
    Email     string    `json:"email"` 
    Password  string    `json:"-"` 
    IsActive  bool      `json:"is_active"` 
    CreatedAt time.Time `json:"created_at"` 
} 
  
// POST — semua field wajib 
type CreateUserRequest struct { 
    Username string `json:"username"` 
    Email    string `json:"email"` 
    Password string `json:"password"` 
} 
  
// PUT — ganti seluruh isi, jadi field bertipe biasa dan semuanya wajib 
type ReplaceUserRequest struct { 
    Username string `json:"username"` 
    Email    string `json:"email"` 
    IsActive bool   `json:"is_active"` 
} 
  
// PATCH — ubah sebagian, jadi field bertipe pointer supaya bisa dibedakan 
// antara "tidak dikirim" (nil) dan "dikirim bernilai kosong" 
type PatchUserRequest struct { 
    Username *string `json:"username,omitempty"` 
    Email    *string `json:"email,omitempty"` 
    IsActive *bool   `json:"is_active,omitempty"` 
} 
  
// Amplop baku untuk semua respons 
type WebResponse struct { 
    Success bool   `json:"success"` 
    Message string `json:"message"` 
    Data    any    `json:"data,omitempty"` 
    Meta    *Meta  `json:"meta,omitempty"` 
    Errors  any    `json:"errors,omitempty"` 
} 
  
type Meta struct { 
    Page       int `json:"page"` 
    Limit      int `json:"limit"` 
    Total      int `json:"total"` 
    TotalPages int `json:"total_pages"` 
} 
  
type ListQuery struct { 
    Page     int 
    Limit    int 
    Search   string 
    Sort     string 
    Order    string 
    IsActive *bool 
}