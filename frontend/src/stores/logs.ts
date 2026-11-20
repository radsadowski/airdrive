import { create } from 'zustand'
import type { AppEventMap } from '@/lib/api/contract'

export type LogLine = AppEventMap['log:line']

const MAX_LINES = 500

interface LogsState {
  lines: LogLine[]
  append: (line: LogLine) => void
  clear: () => void
}

/** Debug log lines streamed from the Go logger while debug logging is on. */
export const useLogsStore = create<LogsState>((set) => ({
  lines: [],

  append(line) {
    set((s) => ({ lines: [...s.lines.slice(-(MAX_LINES - 1)), line] }))
  },

  clear() {
    set({ lines: [] })
  },
}))
