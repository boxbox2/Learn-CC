package contextmgr

const (
	SingleToolResultLimitBytes = 50000
	MessageAggregateLimitBytes = 200000
	SummaryOutputReserveTokens = 20000
	AutoSafetyMarginTokens     = 13000
	ManualSafetyMarginTokens   = 3000
	RecentMinTokens            = 10000
	RecentMinMessages          = 5
	MaxRecentFiles             = 5
	RecentFileSnapshotTokens   = 5000
	AutoSummaryFailureLimit    = 3
	SummaryPTLDirectRetries    = 3
	SummaryPTLDropRatio        = 0.2
	EstimateCharsPerToken      = 3.5
	PreviewMaxBytes            = 2048
	PreviewMaxLines            = 20
)
