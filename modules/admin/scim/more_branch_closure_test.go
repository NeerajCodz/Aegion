package scim

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestSCIMServiceMoreWrapperAndHelperBranches(t *testing.T) {
	ctx := context.Background()
	mockStore := &MockStore{}
	svc := NewService(mockStore, nil)

	t.Run("mapping and token wrapper methods", func(t *testing.T) {
		mapping := &SCIMMapping{Name: "test-mapping"}
		mockStore.On("CreateSCIMMapping", ctx, mock.AnythingOfType("*scim.SCIMMapping")).Return(errors.New("create mapping failed")).Once()
		if _, err := svc.CreateSCIMMapping(ctx, mapping); err == nil || !strings.Contains(err.Error(), "create mapping failed") {
			t.Fatalf("expected create mapping store error, got %v", err)
		}

		if _, err := svc.UpdateSCIMMapping(ctx, &SCIMMapping{Name: "updated"}); !errors.Is(err, ErrRequiredMappingID) {
			t.Fatalf("expected ErrRequiredMappingID, got %v", err)
		}

		update := &SCIMMapping{ID: mustUUID(), Name: "updated"}
		mockStore.On("UpdateSCIMMapping", ctx, mock.AnythingOfType("*scim.SCIMMapping")).Return(errors.New("update mapping failed")).Once()
		if _, err := svc.UpdateSCIMMapping(ctx, update); err == nil || !strings.Contains(err.Error(), "update mapping failed") {
			t.Fatalf("expected update mapping store error, got %v", err)
		}

		mockStore.On("UpdateSCIMMapping", ctx, mock.AnythingOfType("*scim.SCIMMapping")).Return(nil).Once()
		updated, err := svc.UpdateSCIMMapping(ctx, &SCIMMapping{ID: mustUUID(), Name: "  keep-name  "})
		if err != nil {
			t.Fatalf("expected successful mapping update, got %v", err)
		}
		if updated.Name != "keep-name" {
			t.Fatalf("expected trimmed mapping name, got %q", updated.Name)
		}

		mappingID := mustUUID()
		mockStore.On("GetSCIMMapping", ctx, mappingID).Return(&SCIMMapping{ID: mappingID, Name: "retrieved"}, nil).Once()
		gotMapping, err := svc.GetSCIMMapping(ctx, mappingID)
		if err != nil {
			t.Fatalf("GetSCIMMapping returned error: %v", err)
		}
		if gotMapping == nil || gotMapping.ID != mappingID {
			t.Fatalf("unexpected mapping from wrapper: %#v", gotMapping)
		}

		tokens := []*SCIMToken{{Name: "scim-token"}}
		mockStore.On("ListSCIMTokens", ctx).Return(tokens, nil).Once()
		gotTokens, err := svc.ListSCIMTokens(ctx)
		if err != nil {
			t.Fatalf("ListSCIMTokens returned error: %v", err)
		}
		if len(gotTokens) != 1 || gotTokens[0].Name != "scim-token" {
			t.Fatalf("unexpected token list: %#v", gotTokens)
		}

		deleteID := mustUUID()
		mockStore.On("DeleteSCIMToken", ctx, deleteID).Return(nil).Once()
		if err := svc.DeleteSCIMToken(ctx, deleteID); err != nil {
			t.Fatalf("DeleteSCIMToken returned error: %v", err)
		}
	})

	t.Run("filter and mapping helpers", func(t *testing.T) {
		if err := validateUserFilter(nil); err != nil {
			t.Fatalf("validateUserFilter(nil) = %v", err)
		}
		if err := validateUserFilter(&Filter{Attribute: "active", Operator: "eq"}); err != nil {
			t.Fatalf("validateUserFilter(active eq) = %v", err)
		}

		if err := validateGroupFilter(nil); err != nil {
			t.Fatalf("validateGroupFilter(nil) = %v", err)
		}
		if err := validateGroupFilter(&Filter{Attribute: "displayName", Operator: "eq"}); err != nil {
			t.Fatalf("validateGroupFilter(displayName eq) = %v", err)
		}
		if err := validateGroupFilter(&Filter{Attribute: "displayName", Operator: "ne"}); !errors.Is(err, ErrInvalidFilter) {
			t.Fatalf("validateGroupFilter(displayName ne) expected ErrInvalidFilter, got %v", err)
		}

		if got := scimVersionFromTime(time.Time{}); got != "" {
			t.Fatalf("scimVersionFromTime(zero) = %q, want empty", got)
		}
		if _, err := normalizeSCIMMapping(nil, false); !errors.Is(err, ErrRequiredMappingName) {
			t.Fatalf("normalizeSCIMMapping(nil) expected ErrRequiredMappingName, got %v", err)
		}
	})

	mockStore.AssertExpectations(t)
}

func TestSCIMHandlerMoreETagAndDecodeBranches(t *testing.T) {
	ctx := context.Background()
	mockStore := &MockStore{}
	handler := NewHandler(NewService(mockStore, nil))

	t.Run("decode json body rejects multiple JSON objects", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(`{"userName":"u1"}{"extra":true}`))
		rec := httptest.NewRecorder()
		var payload map[string]any
		if err := handler.decodeJSONBody(rec, req, &payload); err == nil {
			t.Fatal("expected decodeJSONBody to reject multiple JSON objects")
		}
	})

	t.Run("write resource etag handles nil writer", func(t *testing.T) {
		handler.writeResourceETag(nil, Meta{Version: "v1"})
	})

	t.Run("if-match helper branches", func(t *testing.T) {
		if !handler.ifMatchSatisfied(Meta{}, "*") {
			t.Fatal("expected wildcard If-Match to pass")
		}
		if handler.ifMatchSatisfied(Meta{}, `W/"v1"`) {
			t.Fatal("expected If-Match with empty resource token to fail")
		}
		meta := Meta{Version: "v2"}
		if !handler.ifMatchSatisfied(meta, `W/"v1", *`) {
			t.Fatal("expected candidate wildcard to pass")
		}
		if !handler.ifMatchSatisfied(meta, `W/"v2"`) {
			t.Fatal("expected matching ETag token to pass")
		}
	})

	t.Run("ensure if-match handles not-found and satisfied branches", func(t *testing.T) {
		userReq := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u1", nil).WithContext(ctx)
		userReq.Header.Set("If-Match", `W/"v1"`)
		userRec := httptest.NewRecorder()
		mockStore.On("GetUserByID", mock.Anything, "u1").Return((*SCIMUser)(nil), errors.New("missing")).Once()
		if handler.ensureUserIfMatch(userRec, userReq, "u1") {
			t.Fatal("expected ensureUserIfMatch to fail for missing user")
		}

		userReq2 := httptest.NewRequest(http.MethodPatch, "/scim/v2/Users/u2", nil).WithContext(ctx)
		userReq2.Header.Set("If-Match", `W/"v2"`)
		mockStore.On("GetUserByID", mock.Anything, "u2").Return(&SCIMUser{ID: "u2", Meta: Meta{Version: "v2"}}, nil).Once()
		if !handler.ensureUserIfMatch(httptest.NewRecorder(), userReq2, "u2") {
			t.Fatal("expected ensureUserIfMatch to pass for matching ETag")
		}

		groupReq := httptest.NewRequest(http.MethodPatch, "/scim/v2/Groups/g1", nil).WithContext(ctx)
		groupReq.Header.Set("If-Match", `W/"v1"`)
		groupRec := httptest.NewRecorder()
		mockStore.On("GetGroupByID", mock.Anything, "g1").Return((*SCIMGroup)(nil), errors.New("missing")).Once()
		if handler.ensureGroupIfMatch(groupRec, groupReq, "g1") {
			t.Fatal("expected ensureGroupIfMatch to fail for missing group")
		}

		groupReq2 := httptest.NewRequest(http.MethodPatch, "/scim/v2/Groups/g2", nil).WithContext(ctx)
		groupReq2.Header.Set("If-Match", `W/"g2v"`)
		mockStore.On("GetGroupByID", mock.Anything, "g2").Return(&SCIMGroup{ID: "g2", Meta: Meta{Version: "g2v"}}, nil).Once()
		if !handler.ensureGroupIfMatch(httptest.NewRecorder(), groupReq2, "g2") {
			t.Fatal("expected ensureGroupIfMatch to pass for matching ETag")
		}
	})

	mockStore.AssertExpectations(t)
}

func mustUUID() uuid.UUID {
	return uuid.New()
}
