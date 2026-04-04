package capability

import (
	"context"
	"errors"
	"testing"
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
