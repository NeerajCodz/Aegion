package capability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestHasRoleCapability_ReturnsFalseOnStoreError(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	roleIDs := []string{"operator"}

	mockStore.On("GetRoles", ctx, roleIDs).Return([]*Role{}, errors.New("store unavailable")).Once()

	if checker.hasRoleCapability(ctx, roleIDs, CapUsersRead) {
		t.Fatalf("expected hasRoleCapability to return false on role lookup error")
	}

	mockStore.AssertExpectations(t)
}

func TestGetEffectiveCapabilities_ReturnsErrorOnAdminLookupFailure(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()
	expectedErr := errors.New("admin lookup failed")

	mockStore.On("GetAdminIdentity", ctx, identityID).Return((*AdminIdentity)(nil), expectedErr).Once()

	caps, err := checker.GetEffectiveCapabilities(ctx, identityID)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected admin lookup error, got %v", err)
	}
	if caps != nil {
		t.Fatalf("expected nil capabilities on error, got %v", caps)
	}

	mockStore.AssertExpectations(t)
}

func TestGetEffectiveCapabilities_GlobalGrantReturnsAllKnownCapabilities(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()
	identityID := uuid.New()

	admin := &AdminIdentity{
		IdentityID: identityID,
		Roles:      []string{},
		Grants:     []Capability{CapAll},
	}

	mockStore.On("GetAdminIdentity", ctx, identityID).Return(admin, nil).Once()
	mockStore.On("GetRoles", ctx, []string{}).Return([]*Role{}, nil).Once()

	caps, err := checker.GetEffectiveCapabilities(ctx, identityID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(caps) != len(AllCapabilityInfo) {
		t.Fatalf("expected %d capabilities, got %d", len(AllCapabilityInfo), len(caps))
	}

	mockStore.AssertExpectations(t)
}

func TestRequireAnyCapabilityMiddleware_Unauthenticated(t *testing.T) {
	checker := NewChecker(&MockStore{})
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	wrapped := checker.RequireAnyCapability(CapUsersRead, CapSystemConfig)(testHandler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("expected handler not to be called for unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestRequireAllCapabilitiesMiddleware_Unauthenticated(t *testing.T) {
	checker := NewChecker(&MockStore{})
	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	wrapped := checker.RequireAllCapabilities(CapUsersRead, CapSystemConfig)(testHandler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("expected handler not to be called for unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestUpdateRole_RejectsInvalidCapability(t *testing.T) {
	mockStore := &MockStore{}
	checker := NewChecker(mockStore)
	ctx := context.Background()

	existing := &Role{ID: "custom_role", IsSystem: false}
	updated := &Role{
		ID:           "custom_role",
		Name:         "Custom",
		Capabilities: []Capability{"users.custom-invalid"},
	}

	mockStore.On("GetRole", ctx, "custom_role").Return(existing, nil).Once()

	err := checker.UpdateRole(ctx, updated)
	if !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("expected ErrInvalidCapability, got %v", err)
	}

	mockStore.AssertExpectations(t)
}

func TestIsValidCapability_DomainWildcardFallback(t *testing.T) {
	checker := &Checker{}

	original, hadOriginal := AllCapabilityInfo[CapUsersAll]
	delete(AllCapabilityInfo, CapUsersAll)
	defer func() {
		if hadOriginal {
			AllCapabilityInfo[CapUsersAll] = original
		}
	}()

	if !checker.isValidCapability(CapUsersAll) {
		t.Fatal("expected users.* wildcard to validate through domain fallback")
	}
}
