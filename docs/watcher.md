# Watcher — how filesystem changes reach the UI

```mermaid
flowchart TD
    subgraph FS ["macOS Filesystem"]
        ROOT["root dir + all subdirs\n(watched recursively)"]
        KERN["macOS FSEvents kernel"]
    end

    subgraph W ["internal/watcher"]
        FSN["fsnotify.Watcher"]
        LOOP["loop() goroutine\nlistens on Events chan"]
        SCHED["schedule(path)\nper-path timer"]
        DEBOUNCE{"debounce\n300 ms\n\nresets if same\npath fires again"}
        HANDLE["handle(path, op)"]
        OP{"op type?"}
    end

    subgraph IDX ["internal/index (SQLite)"]
        UPSERT["UpsertFile()"]
        DEL["DeleteFile() +\nDeleteChildrenOf()"]
        WATCH_NEW["watcher.Add()\nnew subdir"]
    end

    subgraph UI ["Frontend (React)"]
        EVENT["app.Event.Emit\n'fs:changed'"]
        WIRE["events.ts\non('fs:changed')"]
        APPLY["applyFsChange()\napp store"]
        REFRESH["refresh() —\nre-queries SQLite ListDir"]
        RENDER["FileBrowser re-renders"]
    end

    ROOT -->|"kernel events"| KERN
    KERN -->|"chan Event"| FSN
    FSN --> LOOP
    LOOP -->|"schedule()"| SCHED
    SCHED --> DEBOUNCE
    DEBOUNCE -->|"after 300ms silence"| HANDLE
    HANDLE --> OP

    OP -->|"Create"| UPSERT
    OP -->|"Create (dir)"| WATCH_NEW
    OP -->|"Remove / Rename"| DEL
    OP -->|"Write"| UPSERT

    UPSERT -->|"OnChange callback"| EVENT
    DEL    -->|"OnChange callback"| EVENT
    WATCH_NEW -.->|"also re-Watch\nnew subdir"| FSN

    EVENT --> WIRE
    WIRE  --> APPLY
    APPLY --> REFRESH
    REFRESH --> RENDER
```

## Total latency

| Step | Time |
|---|---|
| Debounce | 300 ms |
| SQLite read (`ListDir`) | ~10 ms |
| React re-render | ~16 ms |
| **Total** | **~326 ms** |

## Key design decisions

- **Debounce** — editors write files in multiple rapid bursts; the 300 ms window coalesces them into one index update instead of hammering SQLite.
- **Recursive watching** — `Watch()` walks the entire tree with `filepath.WalkDir` and calls `fsnotify.Add` on every subdirectory. When a new directory is created, `handle()` calls `Add` on it too so newly created subdirs are immediately watched.
- **`DeleteChildrenOf`** — when a directory is deleted from Finder, a single `DELETE WHERE id LIKE 'path/%'` removes the whole subtree from the index in one query.
- **`OnChange` callback** — the watcher knows nothing about Wails; it calls a plain Go function that `main.go` wires to `app.Event.Emit`, keeping the watcher package free of Wails imports.
