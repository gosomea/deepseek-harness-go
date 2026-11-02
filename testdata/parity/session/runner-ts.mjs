// Parity runner that replays a session-surface scenario against the fixed
// TypeScript reference.
//
// Usage: node runner-ts.mjs <dsh-packages-dir> <scenario.json>
//
// Emits one canonical trace line per observation. The format is shared with the Go
// runner so both traces can be compared byte for byte. Message identifiers are
// deliberately not emitted: the reference mints them and this project preserves
// whatever a payload carries, so comparing them would compare a generator rather
// than a behaviour. See testdata/parity/session/README.md for the rules.
//
// <dsh-packages-dir> is the `packages` directory of the fixed reference checkout.
import { readFileSync } from 'node:fs'

const [, , packagesDir, scenarioPath] = process.argv
if (!packagesDir || !scenarioPath) {
  console.error('usage: runner-ts.mjs <dsh-packages-dir> <scenario.json>')
  process.exit(2)
}

const session = await import(new URL('core/session/lib/index.js', `file://${packagesDir}/`).href)
const scenario = JSON.parse(readFileSync(scenarioPath, 'utf8'))

const lines = []
const emit = (line) => lines.push(line)

// The Go runner emits the same fields in the same order. Roles and visible text
// are the model-facing facts; block kinds make a vocabulary difference visible.
const contentText = (content) => (content ?? []).filter((b) => b?.type === 'text').map((b) => b.text).join('')
const contentKinds = (content) => (content ?? []).map((b) => b?.type ?? 'unknown').join(',')
const quote = (text) => JSON.stringify(text)
const seqs = (list) => `[${(list ?? []).map((n) => String(n)).join(',')}]`

// A projection mirroring the Go side's placeholder interpreter: it rewrites the
// target message's content while preserving identity and attribution.
const placeholderOffload = {
  type: 'image/offload',
  project(event, context) {
    const target = event.data.target
    const original = context.events[target]
    const message = original.type === 'user/message' ? original.data : original.data.message
    const rewritten = { ...message, content: [{ type: 'text', text: '[image omitted]' }] }
    return new Map([[target, rewritten]])
  },
}

const projections = scenario.events.some((event) => event.type === 'image/offload') ? [placeholderOffload] : []

let result
try {
  result = session.foldSurface(scenario.events, projections)
} catch (error) {
  if (!scenario.expectFoldError) {
    console.error(`scenario ${scenario.id}: fold failed: ${error.message}`)
    process.exit(1)
  }
  emit('fold=rejected')
  process.stdout.write(lines.join('\n') + '\n')
  process.exit(0)
}
if (scenario.expectFoldError) {
  console.error(`scenario ${scenario.id}: fold succeeded but the scenario declares a rejection`)
  process.exit(1)
}
emit('fold=ok')
emit(`nodes=${seqs(result.nodes)}`)

// foldSurface reports replacements but not the two counters, so they are derived
// here from what it does report: a positional replacement bumps both, and nothing
// else in these scenarios bumps either.
emit(`replaceGeneration=${result.replacements.length}`)
emit(`contentGeneration=${result.replacements.length + (projections.length > 0 ? 1 : 0)}`)
if (result.replacements.length === 0) emit('replacement=none')
for (const replacement of result.replacements) {
  emit(`replacement=seq:${replacement.seq} start:${replacement.start} end:${replacement.end} shadowed:${seqs(replacement.shadowedSeqs)}`)
}

const messages = result.nodes
  .map((seq) => session.deriveEventMessage(scenario.events[seq], result.projectedMessages))
  .filter((message) => message !== null && message !== undefined)
emit(`messages=${messages.length}`)
for (const message of messages) {
  emit(`message role:${message.role} text:${quote(contentText(message.content))} kinds:${contentKinds(message.content)}`)
}

process.stdout.write(lines.join('\n') + '\n')
