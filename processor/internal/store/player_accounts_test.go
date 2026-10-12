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
	if err := s.Add("u1", "User_ABC", "main"); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("u1", "user_abc", ""); err != nil {
		t.Fatalf("player IDs are case-sensitive: %v", err)
	}
	if err := s.Add("u1", "User_ABC", "other"); !errors.Is(err, ErrDuplicateAccount) {
		t.Fatalf("duplicate id: %v", err)
	}
	if err := s.Add("u1", "x", "MAIN"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("labels are case-insensitive: %v", err)
	}
	if err := s.Add("u2", "User_ABC", "main"); err != nil {
		t.Fatalf("same ID on another human: %v", err)
	}
	if err := s.Add("u1", "", ""); !errors.Is(err, ErrInvalidPlayerID) {
		t.Fatalf("empty id: %v", err)
	}
	if err := s.Add("u1", strings.Repeat("x", 257), ""); !errors.Is(err, ErrInvalidPlayerID) {
		t.Fatalf("long id: %v", err)
	}
	if err := s.Add("u1", "y", strings.Repeat("l", 65)); !errors.Is(err, ErrInvalidAccountLabel) {
		t.Fatalf("long label: %v", err)
	}

	a, err := s.Resolve("u1", "Main")
	if err != nil || a == nil || a.PlayerID != "User_ABC" {
		t.Fatalf("resolve by label: %+v %v", a, err)
	}
	a, _ = s.Resolve("u1", "user_abc")
	if a == nil || a.PlayerID != "user_abc" {
		t.Fatalf("resolve exact id before label: %+v", a)
	}
	if a, _ = s.Resolve("u1", "missing"); a != nil {
		t.Fatalf("resolve missing: %+v", a)
	}
	if err := s.SetLabel("u1", "user_abc", "alt"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel("u1", "user_abc", "main"); !errors.Is(err, ErrDuplicateAccountLabel) {
		t.Fatalf("relabel clash: %v", err)
	}
	if err := s.SetLabel("u1", "nope", "x"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("relabel missing: %v", err)
	}
	if err := s.Remove("u1", "User_ABC"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("u1", "User_ABC"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
	list, _ := s.List("u1")
	if len(list) != 1 || list[0].PlayerID != "user_abc" || list[0].Label != "alt" {
		t.Fatalf("list = %+v", list)
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
