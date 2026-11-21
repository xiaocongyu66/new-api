package dto

type UpstreamDTO struct {
	ID       int    `json:"id,omitempty"`
	Name     string `json:"name" binding:"required"`
	BaseURL  string `json:"base_url" binding:"required"`
	Endpoint string `json:"endpoint"`
}

type UpstreamRequest struct {
	ChannelIDs []int64       `json:"channel_ids"`
	Upstreams  []UpstreamDTO `json:"upstreams"`
	Timeout    int           `json:"timeout"`
}

// TestResult 上游测试连通性结果
type TestResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// DifferenceItem 差异项
// Current 为本地值，可能为 nil
// Upstreams 为各渠道的上游值，具体数值 / "same" / nil

type DifferenceItem struct {
	Current    interface{}            `json:"current"`
	Upstreams  map[string]interface{} `json:"upstreams"`
	Confidence map[string]bool        `json:"confidence"`
}

type SyncableChannel struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Status  int    `json:"status"`
	Type    int    `json:"type"`
}

// PricingAnalysisRequest is the request body for POST /api/ratio_sync/analysis.
type PricingAnalysisRequest struct {
	Upstreams []UpstreamDTO `json:"upstreams"`
	Timeout   int           `json:"timeout"`
	Vendor    int           `json:"vendor"`  // 0 = all vendors
	Start     int64         `json:"start"`   // unix seconds, 0 = now-7d
	End       int64         `json:"end"`     // unix seconds, 0 = now
	Windows   []int         `json:"windows"` // context windows; default = standard set
}

// AnalysisVendor is a vendor option for the analysis filter.
type AnalysisVendor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AnalysisChannelWindow is one context window's local vs upstream price for a
// single bound channel.
type AnalysisChannelWindow struct {
	Context   int      `json:"context"`
	Local     *float64 `json:"local"`
	Upstream  *float64 `json:"upstream"`
	DiffPct   *float64 `json:"diff_pct"`
	MarginPct *float64 `json:"margin_pct"`
	// Requests/Cost/Profit aggregate from consume logs whose matched tier falls
	// into this context window. Nil when no log carries that tier.
	Requests *int64   `json:"requests"`
	Cost     *float64 `json:"cost"`
	Profit   *float64 `json:"profit"`
	// Revenue is the quota actually charged (group ratio included); the
	// subscription share and the token-weighted mean group ratio explain how
	// that charge was composed. Nil when no log carries that tier.
	Revenue             *float64 `json:"revenue"`
	SubscriptionRevenue *float64 `json:"subscription_revenue"`
	GroupRatio          *float64 `json:"group_ratio"`
}

// AnalysisChannel is one bound (channel, model) entry within an alias row.
type AnalysisChannel struct {
	ChannelID   int                     `json:"channel_id"`
	ChannelName string                  `json:"channel_name"`
	ChannelType int                     `json:"channel_type"`
	ModelName   string                  `json:"model_name"`
	PriceMode   string                  `json:"price_mode"`
	BillingMode string                  `json:"billing_mode"`
	Requests    int64                   `json:"requests"`
	Tokens      int64                   `json:"tokens"`
	Revenue     float64                 `json:"revenue"`
	Cost        *float64                `json:"cost"`
	Profit      *float64                `json:"profit"`
	MarginPct   *float64                `json:"margin_pct"`
	Windows     []AnalysisChannelWindow `json:"windows"`
	// Channel-wide subscription share and token-weighted group ratio from the
	// consume logs in range. Nil when no logs exist.
	SubscriptionRevenue *float64 `json:"subscription_revenue"`
	GroupRatio          *float64 `json:"group_ratio"`
}

// AnalysisRow is one plaza alias row in the analysis result.
type AnalysisRow struct {
	Alias        string            `json:"alias"`
	PriceModel   string            `json:"price_model"`
	VendorID     int               `json:"vendor_id"`
	VendorName   string            `json:"vendor_name"`
	NameRule     int               `json:"name_rule"`
	MatchedCount int               `json:"matched_count"`
	Requests     int64             `json:"requests"`
	Tokens       int64             `json:"tokens"`
	Revenue      float64           `json:"revenue"`
	Cost         *float64          `json:"cost"`
	Profit       *float64          `json:"profit"`
	MarginPct    *float64          `json:"margin_pct"`
	Channels     []AnalysisChannel `json:"channels"`
}

// PricingAnalysisData is the data payload of POST /api/ratio_sync/analysis.
type PricingAnalysisData struct {
	Windows []int            `json:"windows"`
	Vendors []AnalysisVendor `json:"vendors"`
	Rows    []AnalysisRow    `json:"rows"`
}

// FluctuationChange is one changed price field between two fetch batches.
type FluctuationChange struct {
	Source    string   `json:"source"`
	Model     string   `json:"model"`
	Field     string   `json:"field"`
	OldValue  any      `json:"old_value"`
	NewValue  any      `json:"new_value"`
	ChangePct *float64 `json:"change_pct"`
	FetchedAt int64    `json:"fetched_at"`
}

// FluctuationData is the data payload of GET /api/ratio_sync/fluctuation.
type FluctuationData struct {
	Changes []FluctuationChange `json:"changes"`
}
