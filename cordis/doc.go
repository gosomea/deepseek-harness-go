// Package cordis implements a scoped plugin runtime inspired by Cordis.
//
// Plugins run while their required services are available. Each activation
// owns its services, listeners, child plugins and cleanup functions. Losing a
// dependency unloads that activation; restoring it starts a fresh activation.
//
// Runtime bookkeeping is protected by a mutex. Lifecycle callbacks run serially
// outside that mutex and may register more work. Reentrant or concurrent
// mutations are queued for reconciliation; call Wait from outside lifecycle
// callbacks to observe the settled result. Application services and listener
// bodies are responsible for synchronizing their own data.
package cordis
