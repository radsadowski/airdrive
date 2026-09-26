import { useEffect } from 'react'
import { TooltipProvider } from '@/components/ui/tooltip'
import { AppShell } from '@/views/AppShell'
import { useAppStore } from '@/stores/app'
import { useUiStore } from '@/stores/ui'
import { wireEvents } from '@/lib/events'

export default function App() {
  const init = useAppStore((s) => s.init)
  const theme = useUiStore((s) => s.settings.theme)

  useEffect(() => {
    wireEvents()
    void init()
  }, [init])

  // Apply theme to the root element so [data-theme="light"] CSS overrides kick in.
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  return (
    <TooltipProvider delayDuration={350}>
      <AppShell />
    </TooltipProvider>
  )
}
