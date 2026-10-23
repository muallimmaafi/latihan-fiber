package helper

import "testing"

func newTestSet() *PermissionSet {
	return NewPermissionSet(map[string][]string{
		"admin": {"user:list", "user:delete"},
		"staff": {"user:list"},
		"user":  {},
	})
}

func TestCanGrantsKnownPermission(t *testing.T) {
	p := newTestSet()
	if !p.Can("admin", "user:delete") {
		t.Error("admin seharusnya boleh user:delete")
	}
	if p.Can("staff", "user:delete") {
		t.Error("staff tidak boleh user:delete")
	}
}

// Fail closed: semua yang tidak dikenal ditolak.
func TestCanFailsClosed(t *testing.T) {
	p := newTestSet()
	if p.Can("hantu", "user:list") {
		t.Error("role tidak dikenal harus ditolak")
	}
	if p.Can("admin", "user:lis") {
		t.Error("permission salah ketik harus ditolak")
	}
	var nilSet *PermissionSet
	if nilSet.Can("admin", "user:list") {
		t.Error("PermissionSet nil harus menolak semua")
	}
}

// Role tanpa permission tetap harus dikenal (inilah gunanya LEFT JOIN).
func TestIsKnownRoleWithoutPermissions(t *testing.T) {
	if !newTestSet().IsKnownRole("user") {
		t.Error("role user harus dikenal meski tanpa permission")
	}
}