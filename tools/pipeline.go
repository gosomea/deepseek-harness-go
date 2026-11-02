package tools

import (
	"context"
	"errors"
	"fmt"
)

// Stage names one step of the tool pipeline, in the order they run.
//
// The stages are exported because an observer and a test both need to name what
// happened without depending on the pipeline's internals.
type Stage string

// The ordered stages of one tool execution.
const (
	// StageLookup resolves the requested name to a definition.
	StageLookup Stage = "lookup"
	// StageValidate checks the raw arguments against the schema.
	StageValidate Stage = "validate"
	// StagePreExecute runs admission policy. It is the last stage before any
	// side effect can occur.
	StagePreExecute Stage = "pre-execute"
	// StageExecute runs the tool body.
	StageExecute Stage = "execute"
	// StagePostExecute runs after the body, for normalization that must happen
	// whether the body succeeded or failed.
	StagePostExecute Stage = "post-execute"
	// StageFinalize runs last and may not change the outcome, only annotate it.
	StageFinalize Stage = "finalize"
)

// Observation is one pipeline event, delivered to an observer in order.
//
// It carries the stage rather than a formatted message so a consumer can act on
// it, and so "the body never ran" is checkable by absence of StageExecute
// rather than by parsing text.
type Observation struct {
	// Stage is the stage that produced this event.
	Stage Stage `json:"stage"`
	// Call is the invocation being processed.
	Call Call `json:"call"`
	// Outcome is the outcome at this point, when one has been decided.
	Outcome ResultOutcome `json:"outcome,omitempty"`
	// Detail is a human-readable note for diagnostics.
	Detail string `json:"detail,omitempty"`
}

// Observer receives pipeline events.
//
// An observer cannot influence the outcome: it is notified after each decision,
// so a broken or malicious observer cannot authorize a denied call. An observer
// that panics is contained, because a tracing plugin must not be able to break
// execution.
type Observer interface {
	// Observe receives one event.
	Observe(observation Observation)
}

// ObserverFunc adapts a function to the Observer interface.
type ObserverFunc func(observation Observation)

// Observe implements Observer.
func (f ObserverFunc) Observe(observation Observation) { f(observation) }

// Pipeline executes tool calls through the ordered stages.
//
// It owns no tools and no policy of its own: the registry supplies definitions,
// the guard chain supplies admission, and the pipeline supplies the order. That
// separation is what makes the ordering claim testable.
type Pipeline struct {
	// Registry is the tool source. It is required.
	Registry *Registry
	// Guards is the admission chain, consulted in order. A nil chain admits
	// everything, which is why the field is explicit rather than optional by
	// accident.
	Guards *GuardChain
	// Observer, when set, receives one event per stage.
	Observer Observer
	// PostExecute, when set, runs after the body on every path where the body
	// ran. Its result replaces the body's content, which is how a provider
	// normalizes output without owning the pipeline.
	PostExecute func(result Result) Result
}

// Execute runs one call through every stage.
//
// It always returns exactly one Result and a non-nil error for every outcome
// other than success. The error is returned alongside the Result rather than
// instead of it, because the caller must record a durable result either way:
// a denial that produced no recorded result would be invisible to the model.
func (p *Pipeline) Execute(ctx context.Context, call Call) (Result, error) {
	result := Result{CallID: call.ID, Name: call.Name}
	if err := call.Validate(); err != nil {
		result.Outcome = OutcomeInputInvalid
		result.Content = err.Error()
		result.IsError = true
		result.Err = err
		return result, err
	}

	definition, ok := p.lookup(call, &result)
	if !ok {
		return result, result.Err
	}
	p.emit(StageLookup, call, "", "resolved to "+definition.Name.String())

	arguments, err := ValidateArguments(definition.Schema, call.Arguments)
	if err != nil {
		result.Outcome = OutcomeInputInvalid
		result.IsError = true
		result.Content = err.Error()
		result.Err = err
		p.emit(StageValidate, call, OutcomeInputInvalid, err.Error())
		return result, err
	}
	p.emit(StageValidate, call, "", "arguments satisfy the schema")

	if p.Guards != nil {
		if decision, reason := p.Guards.Check(definition, call); decision == GuardDeny {
			result.Outcome = OutcomeDenied
			result.IsError = true
			if reason == "" {
				reason = "denied by policy"
			}
			result.Content = reason
			result.Err = fmt.Errorf("%w: %s", ErrToolDenied, reason)
			p.emit(StagePreExecute, call, OutcomeDenied, reason)
			return result, result.Err
		}
	}
	p.emit(StagePreExecute, call, "", "admitted by policy")

	if err := ctx.Err(); err != nil {
		// Cancellation is checked immediately before the body, so a caller that
		// cancelled while the arguments were validated does not start work.
		result.Outcome = OutcomeCanceled
		result.IsError = true
		result.Content = err.Error()
		result.Err = err
		p.emit(StageExecute, call, OutcomeCanceled, err.Error())
		return result, err
	}

	result.Attempts = 1
	execution := Context{
		Name:      definition.Name,
		Arguments: arguments,
		Raw:       call.Arguments,
		CallID:    call.ID,
	}
	bodyResult, bodyErr := definition.Execute(execution)
	if bodyErr != nil {
		result.Outcome = OutcomeFailed
		result.IsError = true
		result.Content = bodyErr.Error()
		result.Err = bodyErr
	} else {
		result.Outcome = OutcomeSuccess
		result.IsError = bodyResult.IsError
		result.Content = bodyResult.Content
	}
	p.emit(StageExecute, call, result.Outcome, "")

	// Post-execute runs on every path where the body ran, so normalization
	// cannot be skipped by a failing tool.
	//
	// The hook may replace the model-visible content. Every authoritative fact
	// is restored afterwards, including Err: a normalizer that dropped the
	// underlying error would turn a failed call into a result with a failure
	// outcome but no error, which a caller reads as success.
	if p.PostExecute != nil {
		normalized := p.PostExecute(result)
		normalized.CallID = result.CallID
		normalized.Name = result.Name
		normalized.Outcome = result.Outcome
		normalized.IsError = result.IsError
		normalized.Attempts = result.Attempts
		normalized.Err = result.Err
		result = normalized
	}
	p.emit(StagePostExecute, call, result.Outcome, "")

	// Finalize may annotate but must not change the outcome: the outcome was
	// decided by the stages above, and a later stage rewriting it would make
	// "the final result is the authoritative one" false.
	p.emit(StageFinalize, call, result.Outcome, "")

	if result.Outcome != OutcomeSuccess {
		return result, result.Err
	}
	return result, nil
}

// lookup resolves the requested tool, recording a normalized result when it
// cannot.
func (p *Pipeline) lookup(call Call, result *Result) (Definition, bool) {
	if p.Registry == nil {
		err := fmt.Errorf("%w: pipeline has no registry", ErrInvalidValue)
		result.Outcome = OutcomeFailed
		result.IsError = true
		result.Content = err.Error()
		result.Err = err
		return Definition{}, false
	}
	definition, ok := p.Registry.Lookup(call.Name)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrToolNotFound, call.Name)
		result.Outcome = OutcomeFailed
		result.IsError = true
		result.Content = err.Error()
		result.Err = err
		p.emit(StageLookup, call, OutcomeFailed, err.Error())
		return Definition{}, false
	}
	if definition.Execute == nil {
		err := fmt.Errorf("%w: tool %q has no executor", ErrInvalidValue, call.Name)
		result.Outcome = OutcomeFailed
		result.IsError = true
		result.Content = err.Error()
		result.Err = err
		p.emit(StageLookup, call, OutcomeFailed, err.Error())
		return Definition{}, false
	}
	return definition, true
}

// emit delivers one observation, containing an observer panic.
func (p *Pipeline) emit(stage Stage, call Call, outcome ResultOutcome, detail string) {
	if p.Observer == nil {
		return
	}
	defer func() {
		// A tracing observer must not be able to break execution or authorize
		// anything, so a panic is swallowed here rather than propagated.
		_ = recover()
	}()
	p.Observer.Observe(Observation{Stage: stage, Call: call, Outcome: outcome, Detail: detail})
}

// ExecuteAll runs a sequence of calls, preserving order.
//
// It stops at the first call whose outcome is not success only when stopOnError
// is set; otherwise every call runs and every result is returned, which is what
// a batch of independent tool calls needs.
func (p *Pipeline) ExecuteAll(ctx context.Context, calls []Call, stopOnError bool) ([]Result, error) {
	results := make([]Result, 0, len(calls))
	var errs []error
	for _, call := range calls {
		result, err := p.Execute(ctx, call)
		results = append(results, result)
		if err != nil {
			errs = append(errs, err)
			if stopOnError {
				break
			}
			continue
		}
	}
	return results, errors.Join(errs...)
}

// ValidateResults reports whether a result sequence is a sound answer to a call
// sequence: one result per call, in order, each answering its own call id.
//
// It is the invariant a caller records against, and it is the check that
// catches a pipeline path that produced two results or none.
func ValidateResults(calls []Call, results []Result) error {
	if len(calls) != len(results) {
		return fmt.Errorf("%w: %d calls produced %d results", ErrInvalidValue, len(calls), len(results))
	}
	for index := range calls {
		if calls[index].ID != results[index].CallID {
			return fmt.Errorf("%w: result %d answers call %q, expected %q",
				ErrInvalidValue, index, results[index].CallID, calls[index].ID)
		}
	}
	return nil
}
