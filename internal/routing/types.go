package routing

// InferenceStrategy identifies a routing target.
type InferenceStrategy string

const (
	ProviderLocal = "local"
	ProviderCloud = "cloud"
)

const (
	QuickLocal     InferenceStrategy = "QuickLocal"
	DeepLocal      InferenceStrategy = "DeepLocal"
	CloudGeneral   InferenceStrategy = "CloudGeneral"
	CloudReasoning InferenceStrategy = "CloudReasoning"
	CloudCoding    InferenceStrategy = "CloudCoding"
)

// AllStrategies returns every valid InferenceStrategy.
func AllStrategies() []InferenceStrategy {
	return []InferenceStrategy{
		QuickLocal,
		DeepLocal,
		CloudGeneral,
		CloudReasoning,
		CloudCoding,
	}
}

// LocalModelState tracks the lifecycle of the local model process.
type LocalModelState string

const (
	StateStopped  LocalModelState = "Stopped"
	StateStarting LocalModelState = "Starting"
	StateReady    LocalModelState = "Ready"
	StateStopping LocalModelState = "Stopping"
	StateFaulted  LocalModelState = "Faulted"
)

// RoutingDecision records the outcome of routing a single request.
type RoutingDecision struct {
	Strategy       InferenceStrategy
	Provider       string
	ActualProvider string
	Model          string
	Reason         string
	Confidence     float64
	IsFallback     bool
}

// RequestContext carries per-request metadata for routing.
type RequestContext struct {
	// Model is the client-requested model name, or "auto".
	Model string

	// StrategyOverride is a client-requested strategy, if any.
	StrategyOverride InferenceStrategy

	// IsClassification marks this request as a self-assessment
	// classification that must not trigger further classification.
	IsClassification bool
}

// RoutingSignals holds the extracted signals the routing policy evaluates.
type RoutingSignals struct {
	// ApproximateInputTokens is an estimated input token count.
	ApproximateInputTokens int

	// CodingShaped indicates the request appears to involve coding.
	CodingShaped bool

	// ReasoningShaped indicates the request appears to involve
	// extensive reasoning, research, or comparison.
	ReasoningShaped bool

	// ToolsRequested indicates the request includes tool definitions.
	ToolsRequested bool

	// RequestLength is the character count of the input.
	RequestLength int
}
