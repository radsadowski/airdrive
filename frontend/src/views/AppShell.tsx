import { useEffect, useRef, useState } from 'react'
import { useAppStore } from '@/stores/app'
import { useTransfersStore } from '@/stores/transfers'
import { useUiStore } from '@/stores/ui'
import { copyOrCutSelection, pasteIntoCurrentDir, trashSelection, uploadDroppedFiles } from '@/lib/actions'
import { Sidebar } from '@/components/sidebar'
import { TitleBar } from '@/components/titlebar'
import { Toolbar } from '@/components/toolbar'
import { FileBrowser } from '@/components/browser'
import { Inspector } from '@/components/inspector'
import { TransferDrawer } from '@/components/transfers'
import { SearchOverlay } from '@/components/search'
import { Wizards } from '@/components/wizards'
import { TransferPicker } from '@/components/transfer-picker'
import { SettingsDialog } from '@/components/settings'
import { PromptDialog } from '@/components/prompt'
import { Toasts } from '@/components/toasts'
import { DevPanel } from '@/components/devpanel'
import { Onboarding } from '@/components/onboarding'

function DragOverlay() {
  const [visible, setVisible] = useState(false)
  const depth = useRef(0)

  useEffect(() => {
    const onDragEnter = (e: DragEvent) => {
      if (e.dataTransfer?.types.includes('Files')) {
        depth.current++
        setVisible(true)
      }
    }
    const onDragLeave = () => {
      depth.current = Math.max(0, depth.current - 1)
      if (depth.current === 0) setVisible(false)
    }
    const onDrop = () => {
      depth.current = 0
      setVisible(false)
    }
    window.addEventListener('dragenter', onDragEnter)
    window.addEventListener('dragleave', onDragLeave)
    window.addEventListener('drop', onDrop)
    return () => {
      window.removeEventListener('dragenter', onDragEnter)
      window.removeEventListener('dragleave', onDragLeave)
      window.removeEventListener('drop', onDrop)
    }
  }, [])

  if (!visible) return null
  return (
    <div className="pointer-events-none absolute inset-0 z-[75] flex items-center justify-center bg-amber/[0.06] backdrop-blur-[1px]">
      <div className="animate-pop-in rounded-[12px] border-2 border-dashed border-amber/60 bg-panel/90 px-8 py-6 text-center shadow-2xl">
        <div className="text-[15px] font-semibold text-ink">Drop to upload</div>
        <div className="mt-1 font-mono text-[11px] text-ink-faint">
          files stream into the open folder · MD5 verified
        </div>
      </div>
    </div>
  )
}

export function AppShell() {
  const currentLocationId = useAppStore((s) => s.currentLocationId)
  const inspectorOpen = useUiStore((s) => s.inspectorOpen)

  /* global shortcuts */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey

      // ⌘F: always intercept at capture phase to prevent WKWebView's
      // native find bar from appearing, then open our search overlay.
      if (meta && e.key.toLowerCase() === 'f') {
        e.preventDefault()
        e.stopPropagation()
        useUiStore.getState().setSearchOpen(true)
        return
      }

      // ⌘E — toggle sidebar
      if (meta && e.key.toLowerCase() === 'e') {
        e.preventDefault()
        useUiStore.getState().toggleSidebar()
        return
      }

      // ⌘[ / ⌘] — back / forward (before the input guard so they work everywhere)
      if (meta && e.key === '[') {
        e.preventDefault()
        void useAppStore.getState().goBack()
        return
      }
      if (meta && e.key === ']') {
        e.preventDefault()
        void useAppStore.getState().goForward()
        return
      }

      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return

      if (meta && e.key.toLowerCase() === 'c') {
        copyOrCutSelection('copy')
      } else if (meta && e.key.toLowerCase() === 'x') {
        copyOrCutSelection('cut')
      } else if (meta && e.key.toLowerCase() === 'v') {
        void pasteIntoCurrentDir()
      } else if (e.key === 'Backspace' || e.key === 'Delete') {
        void trashSelection()
      }
    }
    // useCapture=true so ⌘F is caught before the WKWebView native find bar fires.
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [])

  /* hydrate transfers + settings once */
  useEffect(() => {
    void useTransfersStore.getState().hydrate()
    void useUiStore.getState().loadSettings()
  }, [])

  return (
    <div
      className="relative flex h-full w-full flex-col overflow-hidden bg-base"
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes('Files')) e.preventDefault()
      }}
      onDrop={(e) => {
        if (e.dataTransfer.files.length > 0) {
          e.preventDefault()
          uploadDroppedFiles(e.dataTransfer.files)
        }
      }}
    >
      <DragOverlay />
      <TitleBar />
      <div className="relative flex min-h-0 flex-1">
        <Sidebar />

        <main className="relative flex min-w-0 flex-1 flex-col">
          {currentLocationId ? (
            <>
              <Toolbar />
              <div className="relative flex min-h-0 flex-1">
                <FileBrowser />
                {inspectorOpen && <Inspector />}
              </div>
              <TransferDrawer />
            </>
          ) : (
            <Onboarding />
          )}
        </main>
      </div>

      <SearchOverlay />
      <Wizards />
      <TransferPicker />
      <SettingsDialog />
      <PromptDialog />
      <Toasts />
      <DevPanel />
    </div>
  )
}
