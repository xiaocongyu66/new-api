// Package modelapi holds cross-package type aliases for entities that have
// been moved into apps/api/internal/<domain>/model. External callers (oauth,
// service/, controller/, etc.) keep using `modelapi.X` instead of the domain
// package path. When the gin→fiber migration removes the legacy `model` alias
// surface entirely, callers can be updated to use the domain package directly.
package modelapi

import (
	billingmodel "github.com/QuantumNous/new-api/internal/billing/model"
	catalogmodel "github.com/QuantumNous/new-api/internal/catalog/model"
	identitymodel "github.com/QuantumNous/new-api/internal/identity/model"
	taskmodel "github.com/QuantumNous/new-api/internal/task/model"
	usagemodel "github.com/QuantumNous/new-api/internal/usage/model"
)

type (
	// Identity domain
	User                       = identitymodel.User
	Token                      = identitymodel.Token
	PasskeyCredential          = identitymodel.PasskeyCredential
	CustomOAuthProvider        = identitymodel.CustomOAuthProvider
	UserOAuthBinding           = identitymodel.UserOAuthBinding
	AuthFlow                   = identitymodel.AuthFlow
	AuthFlowMatch              = identitymodel.AuthFlowMatch
	AuthFlowCreate             = identitymodel.AuthFlowCreate
	ExternalIdentityClaim      = identitymodel.ExternalIdentityClaim
	UserSession                = identitymodel.UserSession
	UserBase                   = identitymodel.UserBase
	TwoFA                      = identitymodel.TwoFA
	CasbinRule                 = identitymodel.CasbinRule

	// Catalog domain
	Channel                    = catalogmodel.Channel
	Ability                    = catalogmodel.Ability
	Model                      = catalogmodel.Model
	GatewayConfigRevision      = catalogmodel.GatewayConfigRevision
	GatewayConfigOutbox        = catalogmodel.GatewayConfigOutbox
	ProxyNode                  = catalogmodel.ProxyNode
	// Billing domain
	SubscriptionPlan           = billingmodel.SubscriptionPlan
	Checkin                    = billingmodel.Checkin


	// Task domain
	Task                       = taskmodel.Task
	Midjourney                 = taskmodel.Midjourney

	// Usage domain
	QuotaData                  = usagemodel.QuotaData
	PerfMetric                 = usagemodel.PerfMetric
	PerfMetricSummary          = usagemodel.PerfMetricSummary
	PerfMetricSummaryBucket    = usagemodel.PerfMetricSummaryBucket
	Log                        = usagemodel.Log
)
