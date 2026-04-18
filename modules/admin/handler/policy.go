package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	policypb "github.com/aegion/aegion/internal/proto/policy/v1"
	policygrpc "github.com/aegion/aegion/modules/policy/grpc"
	policystore "github.com/aegion/aegion/modules/policy/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PolicyABACRuleRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Expression  string `json:"expression"`
	Priority    int    `json:"priority"`
	Effect      string `json:"effect"`
	Enabled     bool   `json:"enabled"`
}

type PolicyReBACTupleRequest struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace"`
	ObjectID  string `json:"object_id"`
	Relation  string `json:"relation"`
	SubjectID string `json:"subject_id"`
}

type PolicyReBACNamespaceRequest struct {
	ID     string                 `json:"id,omitempty"`
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
	Active bool                   `json:"active"`
}

type PolicySimulationRequest struct {
	Subject      string `json:"subject"`
	Resource     string `json:"resource"`
	ResourceType string `json:"resource_type"`
	Action       string `json:"action"`
	Model        string `json:"model"`
	Context      struct {
		IP       string            `json:"ip"`
		TenantID string            `json:"tenant_id"`
		Extra    map[string]string `json:"extra"`
	} `json:"context"`
}

func (h *Handler) ListPolicyABACRules(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	rows, err := h.dbConn().Query(r.Context(), `
		SELECT id::text, name, description, expression, priority, effect, enabled, created_at, updated_at
		FROM pol_abac_rules
		ORDER BY priority ASC, name ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load ABAC rules")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, name, expression, effect string
			description                  *string
			priority                     int
			enabled                      bool
			createdAt, updatedAt         time.Time
		)
		if err := rows.Scan(&id, &name, &description, &expression, &priority, &effect, &enabled, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ABAC rule")
			return
		}
		items = append(items, map[string]any{
			"id":          id,
			"name":        name,
			"description": description,
			"expression":  expression,
			"priority":    priority,
			"effect":      effect,
			"enabled":     enabled,
			"created_at":  createdAt,
			"updated_at":  updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ABAC rules")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
	h.logAction(r.Context(), &operator.ID, "read", "policy_abac_rule", "*", map[string]any{
		"count": len(items),
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) UpsertPolicyABACRule(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req PolicyABACRuleRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Expression = strings.TrimSpace(req.Expression)
	req.Effect = strings.ToLower(strings.TrimSpace(req.Effect))
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.Name == "" || req.Expression == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name and expression are required")
		return
	}
	if req.Effect != "allow" && req.Effect != "deny" {
		writeError(w, http.StatusBadRequest, "invalid_request", "effect must be allow or deny")
		return
	}

	now := time.Now().UTC()
	var (
		createdAt time.Time
		updatedAt time.Time
	)
	err := h.dbConn().QueryRow(r.Context(), `
		INSERT INTO pol_abac_rules (id, name, description, expression, priority, effect, enabled, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			expression = EXCLUDED.expression,
			priority = EXCLUDED.priority,
			effect = EXCLUDED.effect,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`, req.ID, req.Name, emptyToNil(req.Description), req.Expression, req.Priority, req.Effect, req.Enabled, now, now).Scan(&createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save ABAC rule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          req.ID,
		"name":        req.Name,
		"description": emptyToNil(req.Description),
		"expression":  req.Expression,
		"priority":    req.Priority,
		"effect":      req.Effect,
		"enabled":     req.Enabled,
		"created_at":  createdAt,
		"updated_at":  updatedAt,
	})
	h.logAction(r.Context(), &operator.ID, "upsert", "policy_abac_rule", req.ID, map[string]any{
		"name":     req.Name,
		"effect":   req.Effect,
		"priority": req.Priority,
		"enabled":  req.Enabled,
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) DeletePolicyABACRule(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid ABAC rule id")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM pol_abac_rules WHERE id = $1::uuid`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete ABAC rule")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "ABAC rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	h.logAction(r.Context(), &operator.ID, "delete", "policy_abac_rule", id, nil, IPAddressFromContext(r.Context()))
}

func (h *Handler) ListPolicyReBACTuples(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	rows, err := h.dbConn().Query(r.Context(), `
		SELECT id::text, namespace, object_id, relation, subject_id, created_at
		FROM pol_rebac_tuples
		ORDER BY created_at DESC
		LIMIT 500
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load ReBAC tuples")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, namespace, objectID, relation, subjectID string
			createdAt                                    time.Time
		)
		if err := rows.Scan(&id, &namespace, &objectID, &relation, &subjectID, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ReBAC tuple")
			return
		}
		items = append(items, map[string]any{
			"id":         id,
			"namespace":  namespace,
			"object_id":  objectID,
			"relation":   relation,
			"subject_id": subjectID,
			"created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ReBAC tuples")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
	h.logAction(r.Context(), &operator.ID, "read", "policy_rebac_tuple", "*", map[string]any{
		"count": len(items),
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) ListPolicyReBACNamespaces(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	rows, err := h.dbConn().Query(r.Context(), `
		SELECT id::text, name, config, version, active, created_at, updated_at
		FROM pol_rebac_namespaces
		ORDER BY name ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to load ReBAC namespaces")
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, name             string
			configRaw            []byte
			version              int
			active               bool
			createdAt, updatedAt time.Time
		)
		if err := rows.Scan(&id, &name, &configRaw, &version, &active, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ReBAC namespace")
			return
		}
		config := make(map[string]interface{})
		_ = json.Unmarshal(configRaw, &config)
		items = append(items, map[string]any{
			"id":         id,
			"name":       name,
			"config":     config,
			"version":    version,
			"active":     active,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to read ReBAC namespaces")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
	h.logAction(r.Context(), &operator.ID, "read", "policy_rebac_namespace", "*", map[string]any{
		"count": len(items),
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) UpsertPolicyReBACNamespace(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req PolicyReBACNamespaceRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}
	configRaw, err := json.Marshal(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid namespace config")
		return
	}

	now := time.Now().UTC()
	var (
		id                   string
		version              int
		createdAt, updatedAt time.Time
	)
	err = h.dbConn().QueryRow(r.Context(), `
		INSERT INTO pol_rebac_namespaces (id, name, config, version, active, created_at, updated_at)
		VALUES ($1::uuid, $2, $3::jsonb, 1, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET
			config = EXCLUDED.config,
			version = pol_rebac_namespaces.version + 1,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at
		RETURNING id::text, version, created_at, updated_at
	`, req.ID, req.Name, string(configRaw), req.Active, now, now).Scan(&id, &version, &createdAt, &updatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save ReBAC namespace")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"name":       req.Name,
		"config":     req.Config,
		"version":    version,
		"active":     req.Active,
		"created_at": createdAt,
		"updated_at": updatedAt,
	})
	h.logAction(r.Context(), &operator.ID, "upsert", "policy_rebac_namespace", id, map[string]any{
		"name":    req.Name,
		"version": version,
		"active":  req.Active,
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) DeletePolicyReBACNamespace(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid ReBAC namespace id")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM pol_rebac_namespaces WHERE id = $1::uuid`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete ReBAC namespace")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "ReBAC namespace not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	h.logAction(r.Context(), &operator.ID, "delete", "policy_rebac_namespace", id, nil, IPAddressFromContext(r.Context()))
}

func (h *Handler) UpsertPolicyReBACTuple(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req PolicyReBACTupleRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.ObjectID = strings.TrimSpace(req.ObjectID)
	req.Relation = strings.TrimSpace(req.Relation)
	req.SubjectID = strings.TrimSpace(req.SubjectID)
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.Namespace == "" || req.ObjectID == "" || req.Relation == "" || req.SubjectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "namespace, object_id, relation, and subject_id are required")
		return
	}

	var createdAt time.Time
	err := h.dbConn().QueryRow(r.Context(), `
		INSERT INTO pol_rebac_tuples (id, namespace, object_id, relation, subject_id, created_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			namespace = EXCLUDED.namespace,
			object_id = EXCLUDED.object_id,
			relation = EXCLUDED.relation,
			subject_id = EXCLUDED.subject_id
		RETURNING created_at
	`, req.ID, req.Namespace, req.ObjectID, req.Relation, req.SubjectID, time.Now().UTC()).Scan(&createdAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to save ReBAC tuple")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         req.ID,
		"namespace":  req.Namespace,
		"object_id":  req.ObjectID,
		"relation":   req.Relation,
		"subject_id": req.SubjectID,
		"created_at": createdAt,
	})
	h.logAction(r.Context(), &operator.ID, "upsert", "policy_rebac_tuple", req.ID, map[string]any{
		"namespace":  req.Namespace,
		"object_id":  req.ObjectID,
		"relation":   req.Relation,
		"subject_id": req.SubjectID,
	}, IPAddressFromContext(r.Context()))
}

func (h *Handler) DeletePolicyReBACTuple(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid ReBAC tuple id")
		return
	}
	result, err := h.dbConn().Exec(r.Context(), `DELETE FROM pol_rebac_tuples WHERE id = $1::uuid`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Failed to delete ReBAC tuple")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "ReBAC tuple not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	h.logAction(r.Context(), &operator.ID, "delete", "policy_rebac_tuple", id, nil, IPAddressFromContext(r.Context()))
}

func (h *Handler) SimulatePolicyDecision(w http.ResponseWriter, r *http.Request) {
	operator := OperatorFromContext(r.Context())
	if operator == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	var req PolicySimulationRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.Resource = strings.TrimSpace(req.Resource)
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.Action = strings.TrimSpace(req.Action)
	req.Model = strings.TrimSpace(req.Model)
	req.Context.IP = strings.TrimSpace(req.Context.IP)
	req.Context.TenantID = strings.TrimSpace(req.Context.TenantID)
	if req.Subject == "" || req.ResourceType == "" || req.Action == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "subject, resource_type, and action are required")
		return
	}

	checker := policygrpc.NewServer(policystore.NewWithDB(h.dbConn()))
	response, err := checker.Check(r.Context(), &policypb.CheckRequest{
		Subject:      req.Subject,
		Resource:     req.Resource,
		ResourceType: req.ResourceType,
		Action:       req.Action,
		Model:        req.Model,
		Context: &policypb.Context{
			Ip:       req.Context.IP,
			Time:     timestamppb.New(time.Now().UTC()),
			TenantId: req.Context.TenantID,
			Extra:    req.Context.Extra,
		},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":     response.GetAllowed(),
		"model_used":  response.GetModelUsed(),
		"deny_reason": response.GetDenyReason(),
		"eval_path":   response.GetEvalPath(),
	})
	h.logAction(r.Context(), &operator.ID, "simulate", "policy_decision", req.ResourceType+":"+req.Action, map[string]any{
		"subject":     req.Subject,
		"resource":    req.Resource,
		"model":       req.Model,
		"allowed":     response.GetAllowed(),
		"model_used":  response.GetModelUsed(),
		"deny_reason": response.GetDenyReason(),
	}, IPAddressFromContext(r.Context()))
}

func emptyToNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
