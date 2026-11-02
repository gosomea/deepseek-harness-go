package agentloop

import (
	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/llm"
)

// defaultMaxSteps bounds one turn's steps.
//
// A model that keeps requesting tools would otherwise loop forever. The bound is
// a driver policy, not a model contract, so it lives here with a value large
// enough for real work and small enough to fail fast in a runaway case.
const defaultMaxSteps = 16

// newAgentID mints an Agent identity.
//
// It reuses the message identifier generator so an Agent id has the same shape
// as the identifiers the rest of the project writes, and so a reader needs only
// one format to recognize.
func newAgentID() (agent.ID, error) {
	id, err := llm.NewMessageID()
	if err != nil {
		return "", err
	}
	return agent.ID(id), nil
}
