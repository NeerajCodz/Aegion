package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreachablePostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPostgresStoreErrorPathsOnUnreachableDB(t *testing.T) {
	if _, err := NewPostgres(nil); err == nil {
		t.Fatal("NewPostgres(nil) expected error")
	}

	store, err := NewPostgres(newUnreachablePostgresPool(t))
	if err != nil {
		t.Fatalf("NewPostgres(unreachable pool) error = %v", err)
	}

	now := time.Now().UTC()
	if err := store.SaveEnrollment(Enrollment{ID: "e1", IdentityID: "i1", SecretCiphertext: "cipher", ExpiresAt: now, CreatedAt: now}); err == nil {
		t.Fatal("SaveEnrollment expected error with unreachable DB")
	}
	if _, err := store.GetEnrollment("e1"); err == nil {
		t.Fatal("GetEnrollment expected error with unreachable DB")
	}
	if err := store.DeleteEnrollment("e1"); err == nil {
		t.Fatal("DeleteEnrollment expected error with unreachable DB")
	}

	if err := store.UpsertTOTPFactor(TOTPFactor{IdentityID: "i1", SecretCiphertext: "cipher", EnrolledAt: now, CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("UpsertTOTPFactor expected error with unreachable DB")
	}
	if _, err := store.GetTOTPFactor("i1"); err == nil {
		t.Fatal("GetTOTPFactor expected error with unreachable DB")
	}
	if err := store.UpdateTOTPLastUsed("i1", now); err == nil {
		t.Fatal("UpdateTOTPLastUsed expected error with unreachable DB")
	}

	if err := store.ReplaceBackupCodes("i1", []BackupCode{{ID: "b1", IdentityID: "i1", CodeHash: "hash", BatchID: "batch", CreatedAt: now}}); err == nil {
		t.Fatal("ReplaceBackupCodes expected error with unreachable DB")
	}
	if _, err := store.ListBackupCodes("i1"); err == nil {
		t.Fatal("ListBackupCodes expected error with unreachable DB")
	}
	if err := store.MarkBackupCodeUsed("i1", "b1", now); err == nil {
		t.Fatal("MarkBackupCodeUsed expected error with unreachable DB")
	}

	if err := store.SaveTrustedDevice(TrustedDevice{
		ID:          "d1",
		IdentityID:  "i1",
		TokenHash:   "hash",
		TokenPrefix: "pref",
		Label:       "device",
		ExpiresAt:   now.Add(time.Hour),
		CreatedAt:   now,
	}); err == nil {
		t.Fatal("SaveTrustedDevice expected error with unreachable DB")
	}
	if _, err := store.GetTrustedDevice("i1", "hash", "pref"); err == nil {
		t.Fatal("GetTrustedDevice expected error with unreachable DB")
	}
	if err := store.TouchTrustedDevice("i1", "d1", now); err == nil {
		t.Fatal("TouchTrustedDevice expected error with unreachable DB")
	}
	if err := store.DeleteTrustedDevice("i1", "d1", now); err == nil {
		t.Fatal("DeleteTrustedDevice expected error with unreachable DB")
	}

	if err := store.DeleteAllIdentityData("i1"); err == nil {
		t.Fatal("DeleteAllIdentityData expected error with unreachable DB")
	}
	if _, err := store.ListFactorsByIdentity("i1"); err == nil {
		t.Fatal("ListFactorsByIdentity expected error with unreachable DB")
	}
}
