package webhooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	analytics "github.com/aegion/aegion/modules/analytics"
	"github.com/stretchr/testify/require"
)

type fakeRow struct {
	scanFn func(dest ...interface{}) error
}

func (r fakeRow) Scan(dest ...interface{}) error {
	if r.scanFn == nil {
		return errors.New("no scanFn")
	}
	return r.scanFn(dest...)
}

type fakeRows struct {
	pos  int
	data [][]interface{}
	err  error
}

func (r *fakeRows) Next() bool {
	return r.pos < len(r.data)
}

func (r *fakeRows) Scan(dest ...interface{}) error {
	if r.pos >= len(r.data) {
		return errors.New("scan beyond end")
	}
	row := r.data[r.pos]
	r.pos++
	if len(dest) != len(row) {
		return fmt.Errorf("dest len %d != row len %d", len(dest), len(row))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = row[i].(string)
		case *bool:
			*d = row[i].(bool)
		case *int:
			*d = row[i].(int)
		case *int64:
			*d = row[i].(int64)
		case *time.Time:
			*d = row[i].(time.Time)
		case **time.Time:
			*d = row[i].(*time.Time)
		default:
			return fmt.Errorf("unsupported dest type %T", dest[i])
		}
	}
	return nil
}

func (r *fakeRows) Close() error { return nil }
func (r *fakeRows) Err() error   { return r.err }

type fakeResult struct {
	affected int64
	err      error
}

func (r fakeResult) RowsAffected() (int64, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.affected, nil
}

type fakeDB struct {
	queryRowFn func(ctx context.Context, query string, args ...interface{}) RowScanner
	queryFn    func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error)
	execFn     func(ctx context.Context, query string, args ...interface{}) (ExecResult, error)
}

func (f *fakeDB) QueryRowContext(ctx context.Context, query string, args ...interface{}) RowScanner {
	return f.queryRowFn(ctx, query, args...)
}

func (f *fakeDB) QueryContext(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
	return f.queryFn(ctx, query, args...)
}

func (f *fakeDB) ExecContext(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
	return f.execFn(ctx, query, args...)
}

func TestStore_GetWebhook_NotFound(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error { return sql.ErrNoRows }}
		},
	}

	s := NewStore(db)
	got, err := s.GetWebhook(context.Background(), "missing")
	require.Error(t, err)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestStore_GetWebhook_RoundTripJSONFields(t *testing.T) {
	now := time.Now().UTC()
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error {
				// id, user_id, url, event_types, categories, custom_filter, secret, active, failure_count, created_at, updated_at
				*(dest[0].(*string)) = "wh_1"
				*(dest[1].(*string)) = "user_1"
				*(dest[2].(*string)) = "https://example.test/hook"
				*(dest[3].(*string)) = `["evt.a","evt.b"]`
				*(dest[4].(*string)) = `["cat.a"]`
				*(dest[5].(*string)) = `{"k":"v"}`
				*(dest[6].(*string)) = "secret"
				*(dest[7].(*bool)) = true
				*(dest[8].(*int)) = 2
				*(dest[9].(*time.Time)) = now
				*(dest[10].(*time.Time)) = now
				return nil
			}}
		},
	}

	s := NewStore(db)
	got, err := s.GetWebhook(context.Background(), "wh_1")
	require.NoError(t, err)
	require.Equal(t, "wh_1", got.ID)
	require.Equal(t, "user_1", got.UserID)
	require.Equal(t, []string{"evt.a", "evt.b"}, got.EventTypes)
	require.Equal(t, []string{"cat.a"}, got.Categories)
	require.Equal(t, map[string]interface{}{"k": "v"}, got.CustomFilter)
	require.True(t, got.Active)
	require.Equal(t, 2, got.FailureCount)
}

func TestStore_ListDeliveries_DefaultLimit(t *testing.T) {
	var gotLimit interface{}
	db := &fakeDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			// args: webhookID, limit
			if len(args) >= 2 {
				gotLimit = args[1]
			}
			return &fakeRows{data: [][]interface{}{}}, nil
		},
	}

	s := NewStore(db)
	_, err := s.ListDeliveries(context.Background(), "wh_1", 0)
	require.NoError(t, err)
	require.Equal(t, 100, gotLimit)
}

func TestStore_UpdateWebhook_NoRowsAffected(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			return fakeResult{affected: 0}, nil
		},
	}
	s := NewStore(db)
	err := s.UpdateWebhook(context.Background(), &analytics.Webhook{ID: "wh", UserID: "u"})
	require.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestStore_DeleteWebhook_NoRowsAffected(t *testing.T) {
	db := &fakeDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			return fakeResult{affected: 0}, nil
		},
	}
	s := NewStore(db)
	err := s.DeleteWebhook(context.Background(), "wh", "u")
	require.ErrorIs(t, err, ErrWebhookNotFound)
}

func TestStore_CreateWebhook_MarshalsFieldsAndExecs(t *testing.T) {
	now := time.Now().UTC()
	var gotArgs []interface{}

	db := &fakeDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			gotArgs = args
			return fakeResult{affected: 1}, nil
		},
	}

	s := NewStore(db)
	wh := &analytics.Webhook{
		ID:           "wh_1",
		UserID:       "user_1",
		URL:          "https://example.test/hook",
		EventTypes:   []string{"evt.a", "evt.b"},
		Categories:   []string{"cat.a"},
		CustomFilter: map[string]interface{}{"k": "v"},
		Secret:       "secret",
		Active:       true,
		FailureCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	require.NoError(t, s.CreateWebhook(context.Background(), wh))
	require.GreaterOrEqual(t, len(gotArgs), 11)

	// spot-check key serialized args (order mirrors SQL)
	require.Equal(t, "wh_1", gotArgs[0])
	require.Equal(t, "user_1", gotArgs[1])
	require.Contains(t, gotArgs[3].(string), "evt.a")
	require.Contains(t, gotArgs[4].(string), "cat.a")
	require.Contains(t, gotArgs[5].(string), "\"k\"")
}

func TestStore_ListWebhooks_ParsesRows(t *testing.T) {
	now := time.Now().UTC()
	db := &fakeDB{
		queryFn: func(ctx context.Context, query string, args ...interface{}) (RowsScanner, error) {
			return &fakeRows{data: [][]interface{}{
				// id, user_id, url, event_types, categories, custom_filter, secret, active, failure_count, created_at, updated_at
				{"wh_1", "user_1", "https://example.test/hook", `["evt.a","evt.b"]`, `["cat.a"]`, `{"k":"v"}`, "secret", true, 2, now, now},
			}}, nil
		},
	}

	s := NewStore(db)
	hooks, err := s.ListWebhooks(context.Background(), "user_1")
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	require.Equal(t, "wh_1", hooks[0].ID)
	require.Equal(t, []string{"evt.a", "evt.b"}, hooks[0].EventTypes)
	require.Equal(t, []string{"cat.a"}, hooks[0].Categories)
	require.Equal(t, map[string]interface{}{"k": "v"}, hooks[0].CustomFilter)
	require.Equal(t, 2, hooks[0].FailureCount)
}

func TestStore_GetDelivery_NotFound(t *testing.T) {
	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error { return sql.ErrNoRows }}
		},
	}

	s := NewStore(db)
	got, err := s.GetDelivery(context.Background(), "missing")
	require.Error(t, err)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrDeliveryNotFound)
}

func TestStore_GetDelivery_ScansNullableTimes(t *testing.T) {
	now := time.Now().UTC()
	next := now.Add(5 * time.Minute)
	done := now.Add(10 * time.Minute)

	db := &fakeDB{
		queryRowFn: func(ctx context.Context, query string, args ...interface{}) RowScanner {
			return fakeRow{scanFn: func(dest ...interface{}) error {
				// id, webhook_id, event_id, status, status_code, response_body, error, attempts, max_retries,
				// next_retry_at, last_attempt_at, completed_at, created_at, updated_at
				*(dest[0].(*string)) = "d_1"
				*(dest[1].(*string)) = "wh_1"
				*(dest[2].(*string)) = "evt_1"
				*(dest[3].(*string)) = "success"
				*(dest[4].(*int)) = 204
				*(dest[5].(*string)) = ""
				*(dest[6].(*string)) = ""
				*(dest[7].(*int)) = 1
				*(dest[8].(*int)) = 3
				*(dest[9].(**time.Time)) = &next
				*(dest[10].(*time.Time)) = now
				*(dest[11].(**time.Time)) = &done
				*(dest[12].(*time.Time)) = now
				*(dest[13].(*time.Time)) = now
				return nil
			}}
		},
	}

	s := NewStore(db)
	got, err := s.GetDelivery(context.Background(), "d_1")
	require.NoError(t, err)
	require.NotNil(t, got.NextRetryAt)
	require.NotNil(t, got.CompletedAt)
	require.Equal(t, next, *got.NextRetryAt)
	require.Equal(t, done, *got.CompletedAt)
}

func TestStore_UpdateDelivery_Execs(t *testing.T) {
	var gotArgs []interface{}
	db := &fakeDB{
		execFn: func(ctx context.Context, query string, args ...interface{}) (ExecResult, error) {
			gotArgs = args
			return fakeResult{affected: 1}, nil
		},
	}

	s := NewStore(db)
	now := time.Now().UTC()
	d := &analytics.WebhookDelivery{
		ID:            "d_1",
		WebhookID:     "wh_1",
		EventID:       "evt_1",
		Status:        "retrying",
		StatusCode:    500,
		ResponseBody:  "oops",
		Error:         "err",
		Attempts:      2,
		MaxRetries:    5,
		NextRetryAt:   nil,
		LastAttemptAt: now,
		CompletedAt:   nil,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	require.NoError(t, s.UpdateDelivery(context.Background(), d))
	require.GreaterOrEqual(t, len(gotArgs), 10)
	require.Equal(t, "retrying", gotArgs[0])
	require.Equal(t, 500, gotArgs[1])
	require.Equal(t, "d_1", gotArgs[len(gotArgs)-1])
}
