import type { AppEventMap, AppEventName } from '../contract'

type Handler<K extends AppEventName> = (payload: AppEventMap[K]) => void

/** In-memory event bus — same shape the Wails `Events.On` client implements. */
export class MockEventBus {
  private handlers = new Map<AppEventName, Set<Handler<AppEventName>>>()

  on<K extends AppEventName>(name: K, cb: Handler<K>): () => void {
    let set = this.handlers.get(name)
    if (!set) {
      set = new Set()
      this.handlers.set(name, set)
    }
    set.add(cb as Handler<AppEventName>)
    return () => set!.delete(cb as Handler<AppEventName>)
  }

  emit<K extends AppEventName>(name: K, payload: AppEventMap[K]): void {
    const set = this.handlers.get(name)
    if (!set) return
    for (const h of [...set]) {
      try {
        h(payload)
      } catch (e) {
        console.error('[mock-bus] handler error', e)
      }
    }
  }
}

export const bus = new MockEventBus()
