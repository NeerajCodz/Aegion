// Package policypb contains policy protobuf request/response types.
//
// NOTE: This repository currently checks in lightweight type definitions
// so policy scaffolding can compile in environments without protoc.
// The canonical schema is proto/policy/policy.proto.
package policypb

import timestamppb "google.golang.org/protobuf/types/known/timestamppb"

type Context struct {
	Ip       string                 `json:"ip,omitempty"`
	Time     *timestamppb.Timestamp `json:"time,omitempty"`
	TenantId string                 `json:"tenant_id,omitempty"`
	Extra    map[string]string      `json:"extra,omitempty"`
}

func (x *Context) GetIp() string {
	if x != nil {
		return x.Ip
	}
	return ""
}

func (x *Context) GetTime() *timestamppb.Timestamp {
	if x != nil {
		return x.Time
	}
	return nil
}

func (x *Context) GetTenantId() string {
	if x != nil {
		return x.TenantId
	}
	return ""
}

func (x *Context) GetExtra() map[string]string {
	if x != nil {
		return x.Extra
	}
	return nil
}

type CheckRequest struct {
	Subject      string   `json:"subject,omitempty"`
	Resource     string   `json:"resource,omitempty"`
	ResourceType string   `json:"resource_type,omitempty"`
	Action       string   `json:"action,omitempty"`
	Model        string   `json:"model,omitempty"`
	Context      *Context `json:"context,omitempty"`
}

func (x *CheckRequest) GetSubject() string {
	if x != nil {
		return x.Subject
	}
	return ""
}

func (x *CheckRequest) GetResource() string {
	if x != nil {
		return x.Resource
	}
	return ""
}

func (x *CheckRequest) GetResourceType() string {
	if x != nil {
		return x.ResourceType
	}
	return ""
}

func (x *CheckRequest) GetAction() string {
	if x != nil {
		return x.Action
	}
	return ""
}

func (x *CheckRequest) GetModel() string {
	if x != nil {
		return x.Model
	}
	return ""
}

func (x *CheckRequest) GetContext() *Context {
	if x != nil {
		return x.Context
	}
	return nil
}

type CheckResponse struct {
	Allowed    bool     `json:"allowed,omitempty"`
	ModelUsed  string   `json:"model_used,omitempty"`
	DenyReason string   `json:"deny_reason,omitempty"`
	EvalPath   []string `json:"eval_path,omitempty"`
}

func (x *CheckResponse) GetAllowed() bool {
	if x != nil {
		return x.Allowed
	}
	return false
}

func (x *CheckResponse) GetModelUsed() string {
	if x != nil {
		return x.ModelUsed
	}
	return ""
}

func (x *CheckResponse) GetDenyReason() string {
	if x != nil {
		return x.DenyReason
	}
	return ""
}

func (x *CheckResponse) GetEvalPath() []string {
	if x != nil {
		return x.EvalPath
	}
	return nil
}

type BatchCheckRequest struct {
	Checks []*CheckRequest `json:"checks,omitempty"`
}

func (x *BatchCheckRequest) GetChecks() []*CheckRequest {
	if x != nil {
		return x.Checks
	}
	return nil
}

type BatchCheckResponse struct {
	Results []*CheckResponse `json:"results,omitempty"`
}

func (x *BatchCheckResponse) GetResults() []*CheckResponse {
	if x != nil {
		return x.Results
	}
	return nil
}
