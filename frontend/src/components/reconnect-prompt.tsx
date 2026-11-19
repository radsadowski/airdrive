import { useState } from 'react'
import { useUiStore, toastError } from '@/stores/ui'
import { useAppStore } from '@/stores/app'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

/** Surfaces a dead Google sign-in as soon as its location is opened. */
export function ReconnectPrompt() {
  const prompt = useUiStore((s) => s.reconnectPrompt)
  const dismiss = useUiStore((s) => s.dismissReconnect)
  const resolve = useUiStore((s) => s.resolveReconnect)
  const [busy, setBusy] = useState(false)
  if (!prompt) return null

  const reconnect = async () => {
    setBusy(true)
    try {
      await api.reconnectGdrive(prompt.locationId)
      resolve(prompt.locationId)
      await useAppStore.getState().reloadLocations()
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
          <DialogTitle>Google Drive sign-in expired</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <p className="text-[13px] leading-relaxed text-ink-mut">
            {prompt.name} cannot be reached right now. Sign in again to keep this location, its index and its sync pairs.
          </p>
          {prompt.error && (
            <p className="mt-3 rounded-[6px] border border-hairline bg-raised/60 px-2.5 py-2 font-mono text-[11px] text-ink-faint">
              {prompt.error}
            </p>
          )}
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" disabled={busy} onClick={dismiss}>
            Not now
          </Button>
          <Button disabled={busy} onClick={() => void reconnect()}>
            {busy ? 'Waiting for Google…' : 'Reconnect'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
