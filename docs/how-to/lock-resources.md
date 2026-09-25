# Lock resources

By default, syslet removes every container, network and volume you drop from a CUE repository, including a volume's data.
`#SysdefLock` switches that off for the entries you list (see [Removing specs](../explanation/removing-specs.md) for how syslet handles locked entries).

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
`#SysdefLock` is only a shortcut; you can also set these fields in an entry's `spec` yourself.
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

## Unlock an entry

Remove the name from the list and apply.
Only after that apply can a following change remove or recreate the entry: syslet reads the markers from the installed unit, not from your repository.
