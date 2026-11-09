package generator

const (
	// EvalSessionIDPrefix marks sessions created for evaluation runs. It uses
	// only lowercase letters, digits and hyphens so the IDs are valid Agent
	// Engine session IDs, as in adk-python.
	EvalSessionIDPrefix = "adk-eval-session-"

	// LegacyEvalSessionIDPrefix marks eval sessions created before
	// EvalSessionIDPrefix; IsEvalSessionID still recognises it.
	LegacyEvalSessionIDPrefix = "___eval___session___"

	// DefaultLiveTimeoutSeconds is the default live inference turn timeout.
	DefaultLiveTimeoutSeconds = 300

	defaultEvalUserID = "test_user_id"
)
