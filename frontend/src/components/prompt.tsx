import { useEffect, useRef, useState } from 'react'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export interface PromptRequest {
  title: string
  label: string
  initialValue: string
  confirmLabel: string
  onConfirm: (value: string) => void | Promise<void>
  validate?: (value: string) => string | null
}

let openState: PromptRequest | null = null
const listeners = new Set<(r: PromptRequest | null) => void>()

export function openPrompt(req: PromptRequest): void {
  openState = req
  listeners.forEach((l) => l(openState))
}

export function PromptDialog() {
  const [req, setReq] = useState<PromptRequest | null>(openState)
  const [value, setValue] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    const l = (r: PromptRequest | null) => {
      setReq(r)
      setValue(r?.initialValue ?? '')
      setError(null)
      if (r) setTimeout(() => {
        inputRef.current?.focus()
        inputRef.current?.select()
      }, 30)
    }
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  }, [])

  const submit = async () => {
    if (!req) return
    const v = value.trim()
    if (!v) return
    const validation = req.validate?.(v) ?? null
    if (validation) {
      setError(validation)
      return
    }
    setBusy(true)
    try {
      await req.onConfirm(v)
      openState = null
      listeners.forEach((l) => l(null))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!req) return null

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) {
        openState = null
        listeners.forEach((l) => l(null))
      }
    }}>
      <DialogContent width="max-w-sm">
        <DialogHeader>
          <DialogTitle>{req.title}</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <div className="text-[12px] text-ink-mut mb-2">{req.label}</div>
          <Input
            ref={inputRef}
            value={value}
            onChange={(e) => {
              setValue(e.target.value)
              setError(null)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submit()
              if (e.key === 'Escape') e.stopPropagation()
            }}
            spellCheck={false}
          />
          {error && <div className="mt-2 text-[12px] text-danger">{error}</div>}
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => {
            openState = null
            listeners.forEach((l) => l(null))
          }}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void submit()} disabled={busy || !value.trim()}>
            {req.confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
