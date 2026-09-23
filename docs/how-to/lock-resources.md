# Lock resources

`#SysdefDefaults` lets syslet remove every container, network and volume you drop from a CUE repository, including a volume's data.
`#SysdefLock` switches that off for the entries you list.

## Lock entries

Import the tools package and list the names per type:

```cue
import syslettools "github.com/xchangeee/syslet/schema/tools@v0"

sysdef: (syslettools.#SysdefLock & {in: {
	containers: ["site"]
	networks: ["site-net"]
	volumes: ["site-data"]
}}).out
```

The lock sets `removalAllowed: false` on each entry, and for volumes also `reclaimPolicy: "Retain"`.
Builds and secrets can't be locked.

You can use `#SysdefLock` several times, for example once per file next to the entries it protects; CUE merges the results.

Preview and apply.
The plan only changes `[X-Syslet]` metadata, so nothing restarts:

```text
--- site.container
changed:
  [X-Syslet] RemovalAllowed
    old: true
    new: false
```

## What a lock does

syslet reads the lock from the installed unit file, so it protects an entry even once the entry is gone from the repository:

- A locked container, network or volume whose entry is deleted or disabled is `skipped` and left running, with its data.
- A locked volume refuses changes that would recreate it (see [Manage volumes](manage-volumes.md#change-a-volume)).
- A locked network is still recreated when it changes, since it holds no data.

## Unlock an entry

Remove the name from the list and apply.
Only after that apply can a following change remove or recreate the entry: syslet reads the markers from the installed unit, not from your repository.
