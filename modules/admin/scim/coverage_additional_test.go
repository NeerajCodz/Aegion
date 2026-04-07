package scim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestSCIMService_AdditionalCoverageBranches(t *testing.T) {
	t.Run("new service applies all default config fallbacks", func(t *testing.T) {
		svc := NewService(&NoOpStore{}, nil, Config{})
		if svc.DefaultPageSize() != 20 {
			t.Fatalf("expected default page size 20, got %d", svc.DefaultPageSize())
		}
		if svc.MaxPageSize() != 1000 {
			t.Fatalf("expected max page size 1000, got %d", svc.MaxPageSize())
		}
	})

	t.Run("list groups applies pagination defaults and max clamp", func(t *testing.T) {
		ctx := context.Background()
		mockStore := &MockStore{}
		svc := NewService(mockStore, nil, Config{
			DefaultPageSize: 7,
			MaxPageSize:     9,
		})

		mockStore.On("ListGroups", ctx, (*Filter)(nil), "", SortAscending, 1, 7).Return([]*SCIMGroup{}, 0, nil).Once()
		if _, err := svc.ListGroups(ctx, "", "", SortAscending, 0, 0); err != nil {
			t.Fatalf("ListGroups with defaults returned error: %v", err)
		}

		mockStore.On("ListGroups", ctx, (*Filter)(nil), "", SortAscending, 1, 9).Return([]*SCIMGroup{}, 0, nil).Once()
		if _, err := svc.ListGroups(ctx, "", "", SortAscending, 1, 999); err != nil {
			t.Fatalf("ListGroups with clamped count returned error: %v", err)
		}

		mockStore.AssertExpectations(t)
	})

	t.Run("create token fails when lookup prefix cannot be derived", func(t *testing.T) {
		svc := NewService(&NoOpStore{}, nil, Config{
			TokenPrefix:          "aegion_scim_",
			TokenLookupPrefixLen: 12,
			TokenEntropyBytes:    1,
		})
		_, _, err := svc.CreateSCIMToken(context.Background(), "tiny", "", nil, nil, uuid.New())
		if !errors.Is(err, ErrInvalidTokenLength) {
			t.Fatalf("expected ErrInvalidTokenLength, got %v", err)
		}
	})

	t.Run("create token propagates store error", func(t *testing.T) {
		ctx := context.Background()
		mockStore := &MockStore{}
		svc := NewService(mockStore, nil)
		mockStore.On("CreateSCIMToken", ctx, mock.AnythingOfType("*scim.SCIMToken")).Return(errors.New("store failed")).Once()

		_, _, err := svc.CreateSCIMToken(ctx, "store-error", "", []string{"users:read"}, nil, uuid.New())
		if err == nil || !strings.Contains(err.Error(), "store failed") {
			t.Fatalf("expected propagated store error, got %v", err)
		}
		mockStore.AssertExpectations(t)
	})

	t.Run("validate token succeeds even if last-used update fails", func(t *testing.T) {
		ctx := context.Background()
		mockStore := &MockStore{}
		svc := NewService(mockStore, nil, Config{
			TokenLastUsedUpdateTimeout: 5 * time.Millisecond,
		})

		tokenString := "aegion_scim_1234567890abcdefghijklmn"
		prefix := tokenString[12:24]
		hash := sha256.Sum256([]byte(tokenString))
		token := &SCIMToken{
			ID:        uuid.New(),
			Prefix:    prefix,
			TokenHash: base64.StdEncoding.EncodeToString(hash[:]),
			Active:    true,
		}

		mockStore.On("GetSCIMTokenByPrefix", ctx, prefix).Return(token, nil).Once()
		mockStore.On("UpdateSCIMTokenLastUsed", mock.Anything, token.ID).Return(errors.New("update failed")).Once()

		got, err := svc.ValidateToken(ctx, tokenString)
		if err != nil {
			t.Fatalf("ValidateToken returned error: %v", err)
		}
		if got == nil || got.ID != token.ID {
			t.Fatalf("expected validated token ID %s, got %+v", token.ID, got)
		}

		time.Sleep(15 * time.Millisecond)
		mockStore.AssertExpectations(t)
	})
}

func TestSCIMHandler_AdditionalCoverageBranches(t *testing.T) {
	t.Run("new handler falls back to service and package defaults", func(t *testing.T) {
		svc := NewService(&NoOpStore{}, nil, Config{
			DefaultPageSize: 33,
			MaxPageSize:     44,
		})
		handlerFromService := NewHandler(svc, HandlerConfig{})
		if handlerFromService.config.DefaultPageSize != 33 {
			t.Fatalf("expected default page size from service, got %d", handlerFromService.config.DefaultPageSize)
		}
		if handlerFromService.config.MaxPageSize != 44 {
			t.Fatalf("expected max page size from service, got %d", handlerFromService.config.MaxPageSize)
		}

		handlerDefaults := NewHandler(nil, HandlerConfig{})
		if handlerDefaults.config.DefaultPageSize != 20 {
			t.Fatalf("expected package default page size, got %d", handlerDefaults.config.DefaultPageSize)
		}
		if handlerDefaults.config.MaxPageSize != 1000 {
			t.Fatalf("expected package default max page size, got %d", handlerDefaults.config.MaxPageSize)
		}
	})

	t.Run("patch user returns internal error for unexpected service failure", func(t *testing.T) {
		mockStore := &MockStore{}
		svc := NewService(mockStore, nil)
		handler := NewHandler(svc)

		req := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u1", bytes.NewBufferString(`{
			"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],
			"Operations":[{"op":"replace","path":"active","value":true}]
		}`))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "u1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()

		mockStore.On("PatchUser", mock.Anything, "u1", mock.Anything).Return(nil, errors.New("db down")).Once()
		handler.PatchUser(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected %d, got %d", http.StatusInternalServerError, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "internalError") {
			t.Fatalf("expected internal error SCIM response, got %q", rec.Body.String())
		}
		mockStore.AssertExpectations(t)
	})

	t.Run("pagination clamps count to max page size", func(t *testing.T) {
		handler := NewHandler(nil, HandlerConfig{
			DefaultPageSize: 2,
			MaxPageSize:     5,
		})
		req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users?startIndex=0&count=99", nil)

		start, count := handler.parsePagination(req)
		if start != 1 {
			t.Fatalf("expected startIndex 1, got %d", start)
		}
		if count != 5 {
			t.Fatalf("expected clamped count 5, got %d", count)
		}
	})
}
