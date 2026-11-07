package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// runPlayerAccountStoreContract holds every PlayerAccountStore implementation
// (mock and SQL) to one behavioural contract. The store must be empty for
// humans "u1" and "u2".
func runPlayerAccountStoreContract(t *testing.T, s PlayerAccountStore) {
	t.Helper()
	if err := s.Add("u1", "UserABC9", "main"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("u1", "userabc9", ""); err != nil {
		t.Fatalf("player IDs are case-sensitive: %v", err)
	}
	if err := s.Add("u1", "UserABC9", "other"); !errors.Is(err, ErrDuplicateAccount) {
		t.Fatalf("duplicate id: %v", err)
	}
	if err := s.Add("u1", "x", "MAIN"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("labels are case-insensitive: %v", err)
	}
	if err := s.Add("u2", "UserABC9", "main"); err != nil {
		t.Fatalf("same ID on another human: %v", err)
	}
	for _, bad := range []string{"", "User_ABC", "a b", "ab:c", "é", "\u201cabc\u201d", "abc\u200b", strings.Repeat("a", 257)} {
		if err := s.Add("u1", bad, ""); !errors.Is(err, ErrInvalidPlayerID) {
			t.Fatalf("id %q: %v", bad, err)
		}
	}
	if err := s.Add("u1", strings.Repeat("a", 256), ""); err != nil {
		t.Fatalf("256-char id: %v", err)
	}
	if err := s.Remove("u1", strings.Repeat("a", 256)); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("u1", "y", strings.Repeat("l", 65)); !errors.Is(err, ErrInvalidAccountLabel) {
		t.Fatalf("long label: %v", err)
	}

	a, err := s.Resolve("u1", "Main")
	if err != nil || a == nil || a.PlayerID != "UserABC9" {
		t.Fatalf("resolve by label: %+v %v", a, err)
	}
	a, _ = s.Resolve("u1", "userabc9")
	if a == nil || a.PlayerID != "userabc9" {
		t.Fatalf("resolve exact id before label: %+v", a)
	}
	if a, _ = s.Resolve("u1", "missing"); a != nil {
		t.Fatalf("resolve missing: %+v", a)
	}
	if err := s.SetLabel("u1", "userabc9", "alt"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel("u1", "userabc9", "main"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("relabel clash: %v", err)
	}
	if err := s.SetLabel("u1", "userabc9", "MAIN"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("relabel clash is case-insensitive: %v", err)
	}
	// Re-casing an account's own label is not a clash.
	if err := s.SetLabel("u1", "userabc9", "ALT"); err != nil {
		t.Fatalf("relabel to different case of own label: %v", err)
	}
	if err := s.SetLabel("u1", "userabc9", "alt"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel("u1", "nope", "x"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("relabel missing: %v", err)
	}
	if err := s.Remove("u1", "UserABC9"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("u1", "UserABC9"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	list, _ := s.List("u1")
	if len(list) != 1 || list[0].PlayerID != "userabc9" || list[0].Label != "alt" {
		t.Fatalf("list = %+v", list)
	}
}

// runPlayerAccountStoreLongIDContract checks that a webhook human, whose ID
// is its (long) webhook URL, can link accounts.
func runPlayerAccountStoreLongIDContract(t *testing.T, s PlayerAccountStore) {
	t.Helper()
	long := "https://discord.com/api/webhooks/" + strings.Repeat("w", 200-len("https://discord.com/api/webhooks/"))
	if len(long) != 200 {
		t.Fatalf("test id length %d", len(long))
	}
	if err := s.Add(long, "Player1", "main"); err != nil {
		t.Fatalf("add for 200-char human id: %v", err)
	}
	a, err := s.Resolve(long, "main")
	if err != nil || a == nil || a.PlayerID != "Player1" || a.ID != long {
		t.Fatalf("resolve for 200-char human id: %+v %v", a, err)
	}
	list, err := s.List(long)
	if err != nil || len(list) != 1 {
		t.Fatalf("list for 200-char human id: %+v %v", list, err)
	}
}

// runPlayerAccountStoreLimitContract expects an empty store for human "lim".
func runPlayerAccountStoreLimitContract(t *testing.T, s PlayerAccountStore) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if err := s.Add("lim", fmt.Sprint("p", i), ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add("lim", "p50", ""); !errors.Is(err, ErrTooManyAccounts) {
		t.Fatalf("51st account: %v", err)
	}
}

func TestMockPlayerAccounts(t *testing.T) {
	runPlayerAccountStoreContract(t, NewMockPlayerAccountStore())
}

func TestMockPlayerAccountsLimit(t *testing.T) {
	runPlayerAccountStoreLimitContract(t, NewMockPlayerAccountStore())
}

func TestMockPlayerAccountsLongID(t *testing.T) {
	runPlayerAccountStoreLongIDContract(t, NewMockPlayerAccountStore())
}
