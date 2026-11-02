// Package taskstate records the durable state of long-running work: a goal, a
// plan and a todo list.
//
// The package exists because these three things are often conflated with a chat
// message, and that conflation loses the thing that matters: a goal survives a
// turn boundary, a plan is a mode rather than a message, and a todo list is a
// projection of events rather than the events themselves. All three are recorded
// as events so that a reader can rebuild the current state from the log, and the
// projection is a pure function of those events rather than a second source of
// truth.
//
// The three are kept in one package because they share the same fact — "the
// model's stated intention, versioned" — and splitting them would duplicate the
// event/replay machinery three times.
package taskstate
