// Parity runner that replays a scenario against the fixed Cordis TypeScript build.
//
// Usage: node runner-ts.mjs <cordis-lib-index.js> <scenario.json>
//
// Emits one canonical trace line per observable event. The format is shared
// with the Go runner so both traces can be compared byte for byte. Timestamps,
// fiber uids and registration order of independent nodes are deliberately not
// emitted: see testdata/parity/cordis/README.md for the normalization rules.
import { readFileSync } from 'node:fs'

const [, , libPath, scenarioPath] = process.argv
if (!libPath || !scenarioPath) {
  console.error('usage: runner-ts.mjs <cordis-lib> <scenario.json>')
  process.exit(2)
}

const { Context } = await import(libPath)
const scenario = JSON.parse(readFileSync(scenarioPath, 'utf8'))

const STATES = { 0: 'pending', 1: 'loading', 2: 'active', 3: 'failed', 4: 'disposed', 5: 'unloading' }
const lines = []
const nodes = new Map()
const ready = new Map()

const state = (fiber) => (fiber === undefined || fiber === null ? 'absent' : STATES[fiber.state] ?? String(fiber.state))
const emit = (line) => lines.push(line)
const settle = () => new Promise((resolve) => setTimeout(resolve, 25))

const root = new Context()

function register(step) {
  const inject = step.inject ?? []
  const plugin = {
    name: step.name,
    inject,
    apply(ctx) {
      emit(`apply ${step.as}`)
      for (const label of step.provides ?? []) {
        ctx.reflect.provide(label, label, step.ready === undefined ? undefined : () => ready.get(step.as) === true)
      }
      for (const [index, label] of (step.cleanups ?? []).entries()) {
        ctx.effect(() => {
          return () => {
            if (label === 'panic') {
              emit(`cleanup ${step.as}:${label}`)
              throw new Error('cleanup panic')
            }
            emit(`cleanup ${step.as}:${label}`)
          }
        })
      }
      for (const listener of step.listeners ?? []) {
        const options = { prepend: listener.prepend === true }
        ctx.on(listener.event, (...args) => {
          if (listener.kind === 'bail') {
            emit(`listener ${step.as}:${listener.seq}:bail=${listener.value}`)
            return listener.value
          }
          emit(`listener ${step.as}:${listener.seq}`)
        }, options)
      }
      if (step.selfDispose) ctx.fiber.dispose()
      return () => emit(`cleanup ${step.as}:returned`)
    },
  }
  if (step.validate) {
    // Mirrors the project's own explicit Validate hook: a config whose `n`
    // value starts with "good" is accepted, anything else is rejected.
    plugin.Config = {
      '~standard': {
        vendor: 'parity',
        version: 1,
        validate(value) {
          return typeof value?.n === 'string' && value.n.startsWith('good')
            ? { value }
            : { issues: [{ message: 'n must start with good' }] }
        },
      },
    }
  }
  const fiber = root.plugin(plugin, step.config)
  nodes.set(step.as, fiber)
}

await (async () => {
  for (const [index, step] of scenario.steps.entries()) {
    const n = index + 1
    switch (step.op) {
      case 'register':
        register(step)
        break
      case 'settle':
        await settle()
        break
      case 'observe': {
        emit(`observe ${step.note}`)
        for (const [as, fiber] of nodes) emit(`state ${as}=${state(fiber)}`)
        break
      }
      case 'setReady':
        ready.set(step.as, step.ready)
        break
      case 'notify':
        root.reflect.notify(step.names)
        break
      case 'dispose':
        await nodes.get(step.as).dispose()
        emit(`disposed ${step.as}`)
        break
      case 'close':
        try {
          await root.fiber.dispose()
          emit('close ok')
        } catch (error) {
          emit(`close error=${error.message}`)
        }
        break
      case 'emit':
        root.emit(step.event, 'x')
        break
      case 'serial':
        await root.serial(step.event, 'x')
        break
      case 'bail':
        root.bail(step.event, 'x')
        break
      case 'waterfall':
        await root.waterfall(step.event, 'x', () => 'default')
        break
      case 'update':
        try {
          nodes.get(step.as).update(step.config)
          await settle()
          emit(`update ${step.as}=ok`)
        } catch (error) {
          emit(`update ${step.as}=rejected`)
        }
        break
      default:
        throw new Error(`unknown op ${step.op} at step ${n}`)
    }
  }
})()

process.stdout.write(lines.join('\n') + '\n')
