# Interface providers

This fork's module path is `github.com/jannson/anet`. The work is on `dev`.
Applications retain the existing query functions and may install a process-wide
native provider before network modules start:

```go
type InterfaceInfo struct {
    Interface net.Interface
    Addresses []net.Addr
}
type InterfaceProvider interface {
    Snapshot() ([]InterfaceInfo, error)
}

anet.SetInterfaceProvider(provider)
interfaces, err := anet.Interfaces()
iface, err := anet.InterfaceByName("wlan0")
iface, err = anet.InterfaceByIndex(16)
addresses, err := anet.InterfaceAddrsByInterface(iface)
allAddresses, err := anet.InterfaceAddrs()
snapshot, err := anet.Snapshot()
```

`Snapshot` gives interfaces and addresses from one provider call. Names and
positive indices must be unique. Provider addresses may be `*net.IPNet` or
`*net.IPAddr`; names, flags, MTU and addresses describe the same observation.
Do not collect hardware MAC addresses in the Android adapter.

The provider must support concurrent calls and return promptly. Publish fresh
or immutable snapshots: do not mutate published slices, IPs, masks or hardware
addresses in place. Queries validate and copy records before returning them to
callers. Callbacks run outside the registration lock. They must not dispatch
synchronously to a UI thread that is waiting for the query, or re-enter a
waiting SDK operation.

An empty snapshot is a successful offline result. Provider errors, malformed
snapshots and invalid native JSON are errors, not a reason to use system
enumeration. `SetInterfaceProvider(nil)` explicitly restores the platform
implementation. Registration is independent of an Activity, login or Runtime;
replacing the provider does not close the previous object, and in-flight calls
finish against the provider they selected.

Use these functions throughout the consumer. Calling `net.Interface.Addrs`,
`net.InterfaceByName` or `net.Interfaces` directly still reaches Go's standard
library and bypasses the provider. This module does not modify private Go
runtime symbols or the standard library's IPv6 zone cache.

## gomobile / Android

`github.com/jannson/anet/mobile` provides a binding-compatible adapter:

```go
type InterfaceProvider interface {
    SnapshotJSON() (string, error)
}
mobile.SetInterfaceProvider(nativeProvider)
json, err := mobile.InterfaceSnapshotJSON()
```

The `mobile` adapter owns JSON decoding and validation; Go network modules keep
using typed `anet` queries. Version 1 is:

```json
{
  "version": 1,
  "interfaces": [
    {
      "index": 16,
      "name": "wlan0",
      "mtu": 1500,
      "up": true,
      "multicast": true,
      "broadcast": true,
      "addresses": ["192.0.2.12/24", "fe80::12/64"]
    }
  ]
}
```

Optional booleans are `running`, `loopback`, `pointToPoint` and `broadcast`.
Omitted booleans are false. Address strings are CIDRs preserving the host IP,
or IP literals when the platform does not know the prefix. IPv6 zone names are
not part of these strings; the interface's name/index identifies the scope.
`{"version":1,"interfaces":[]}` is the explicit offline snapshot. A missing or
null interfaces array is an error. The JSON limit is 1 MiB. Native exceptions
propagate through the query; payloads are not included in parse-error text.

The apptunnel SDK exposes this as `easysdk.NetworkInterfaceProvider` plus
`Easysdk.setNetworkInterfaceProvider` and `networkInterfaceSnapshotJSON`.
`AndroidNetworkInterfaces` in apptunnel's `mobile-native/android` implements the
callback using Java `NetworkInterface`. Install it before constructing the SDK.
It contains no Activity or Context reference, permission dialog or network RPC.
Android permissions and multicast receive locks remain owned by the host.

The prebuilt demonstration files under `mobile/libs` are historical binaries;
they were not regenerated for this change. Build the current source or use the
verified SDK candidate recorded by the consuming project.

## Platform default and upstream selection

Without registration, non-Android platforms use Go's system queries. Android
uses the existing RTM_GETADDR/ioctl workaround on Android 11+; older versions use
system queries. CGO builds can detect the device API level. If detection is
unavailable, the Android workaround is retained. The legacy
`SetAndroidVersion` takes the Android release major, not the API level; zero
restores automatic selection, and concurrent setters are safe.

Upstream inspected: `wlynxg/anet` main
`5501d401a269290292909e6cc75f105571f97cfa`.

| Upstream area | Decision |
| --- | --- |
| Android version comparison, `ace4c22` | Incorporate corrected selection semantics |
| Runtime API level detection, `4173812` | Copy the two API-level implementation files |
| Name/index queries | Implement through the common provider interface |
| `net.zoneCache` and x/net zone-cache linkname changes | Do not incorporate private-symbol hooks |
| Go 1.23+ `-checklinkname=0` requirement | Not required by this implementation |

This is selective source integration, not a Git merge of the complete upstream
history. The jannson module identity is retained for existing SDK imports. This
work does not claim to fix every Go IPv6 scoped-address issue in #68082.
