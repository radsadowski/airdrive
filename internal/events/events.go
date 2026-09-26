// Package events defines the Wails event name constants shared across all
// Go packages that emit or register events. A single source of truth means
// a typo is a compile error, not a silent missing event.
//
// The same names must also match the TypeScript AppEventMap in
// frontend/src/lib/api/contract.ts — update both together when adding events.
package events

const (
	AppReady          = "app:ready"
	IndexProgress     = "index:progress"
	FsChanged         = "fs:changed"
	LocationStatus    = "location:status"
	TransferUpdated   = "transfer:updated"
	TransferBatchDone = "transfer:batchDone"
	ToastShow         = "toast:show"
)
