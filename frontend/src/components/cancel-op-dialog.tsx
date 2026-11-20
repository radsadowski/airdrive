import { useState } from 'react'
import { api } from '@/lib/api'
import { toastError, useUiStore } from '@/stores/ui'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

/** Confirms cancelling a running file operation and states the risk up front. */
export function CancelOpDialog() {
  const prompt = useUiStore((s) => s.cancelPrompt)
  const dismiss = useUiStore((s) => s.dismissCancelOp)
  const [busy, setBusy] = useState(false)
  if (!prompt) return null

  const confirm = async () => {
    setBusy(true)
    try {
      await api.cancelFileOp(prompt.opId)
      dismiss()
    } catch (e) {
      toastError(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) dismiss()
      }}
    >
      <DialogContent width="max-w-md">
        <DialogHeader>
          <DialogTitle>Cancel this operation?</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <p className="text-[13px] leading-relaxed text-ink-mut">
            Stopping {prompt.label} part-way can corrupt the state of this location: items already processed stay
            processed, the rest keep their old state, and the index can disagree with the provider until the location
            is indexed again.
          </p>
          <p className="mt-2 text-[12px] leading-relaxed text-ink-faint">
            Cancel only if you can re-check the folder afterwards.
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" disabled={busy} onClick={dismiss}>
            Keep running
          </Button>
          <Button variant="danger" disabled={busy} onClick={() => void confirm()}>
            Cancel operation
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
