package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/jannson/anet"
)

// InterfaceProvider is implemented by a native host through gomobile/JNI.
// SnapshotJSON returns the version-1 interface snapshot documented in README.
// It must be thread-safe, return promptly, and must not synchronously wait for
// the UI thread or re-enter an SDK operation that waits on this query.
type InterfaceProvider interface {
	SnapshotJSON() (string, error)
}

type nativeProvider struct{ source InterfaceProvider }

type wireSnapshot struct {
	Version    int              `json:"version"`
	Interfaces *[]wireInterface `json:"interfaces"`
}

type wireInterface struct {
	Index        int      `json:"index"`
	Name         string   `json:"name"`
	MTU          int      `json:"mtu"`
	Up           bool     `json:"up"`
	Running      bool     `json:"running,omitempty"`
	Loopback     bool     `json:"loopback,omitempty"`
	Multicast    bool     `json:"multicast"`
	Broadcast    bool     `json:"broadcast,omitempty"`
	PointToPoint bool     `json:"pointToPoint,omitempty"`
	Addresses    []string `json:"addresses"`
}

// SetInterfaceProvider adapts a gomobile-compatible callback to anet's typed
// provider. Register once for the Application lifetime; nil restores the
// platform implementation. No Activity or SDK Runtime owns this registration.
func SetInterfaceProvider(provider InterfaceProvider) {
	if provider == nil {
		anet.SetInterfaceProvider(nil)
		return
	}
	anet.SetInterfaceProvider(nativeProvider{source: provider})
}

func (p nativeProvider) Snapshot() ([]anet.InterfaceInfo, error) {
	data, err := p.source.SnapshotJSON()
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("native interface snapshot exceeds 1 MiB")
	}
	var snapshot wireSnapshot
	if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
		return nil, errors.New("invalid native interface snapshot JSON")
	}
	if snapshot.Version != 1 || snapshot.Interfaces == nil {
		return nil, errors.New("native interface snapshot requires version 1 and interfaces array")
	}
	out := make([]anet.InterfaceInfo, 0, len(*snapshot.Interfaces))
	for _, record := range *snapshot.Interfaces {
		iface := net.Interface{Index: record.Index, Name: record.Name, MTU: record.MTU}
		for _, flag := range []struct {
			set   bool
			value net.Flags
		}{
			{record.Up, net.FlagUp}, {record.Running, net.FlagRunning}, {record.Loopback, net.FlagLoopback},
			{record.Multicast, net.FlagMulticast}, {record.Broadcast, net.FlagBroadcast}, {record.PointToPoint, net.FlagPointToPoint},
		} {
			if flag.set {
				iface.Flags |= flag.value
			}
		}
		item := anet.InterfaceInfo{Interface: iface}
		for _, text := range record.Addresses {
			if ip, network, err := net.ParseCIDR(text); err == nil {
				network.IP = ip // preserve the host address, not only its network.
				item.Addresses = append(item.Addresses, network)
			} else if ip := net.ParseIP(text); ip != nil {
				item.Addresses = append(item.Addresses, &net.IPAddr{IP: ip})
			} else {
				return nil, fmt.Errorf("invalid native address on interface index %d", record.Index)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// InterfaceSnapshotJSON queries the same provider used by anet consumers.
// It is suitable for gomobile callers without exposing net.Interface types.
func InterfaceSnapshotJSON() (string, error) {
	snapshot, err := anet.Snapshot()
	if err != nil {
		return "", err
	}
	interfaces := make([]wireInterface, 0, len(snapshot))
	for _, item := range snapshot {
		iface := item.Interface
		record := wireInterface{Index: iface.Index, Name: iface.Name, MTU: iface.MTU,
			Up: iface.Flags&net.FlagUp != 0, Running: iface.Flags&net.FlagRunning != 0,
			Loopback: iface.Flags&net.FlagLoopback != 0, Multicast: iface.Flags&net.FlagMulticast != 0,
			Broadcast: iface.Flags&net.FlagBroadcast != 0, PointToPoint: iface.Flags&net.FlagPointToPoint != 0,
			Addresses: make([]string, 0, len(item.Addresses))}
		for _, address := range item.Addresses {
			record.Addresses = append(record.Addresses, address.String())
		}
		interfaces = append(interfaces, record)
	}
	data, err := json.Marshal(wireSnapshot{Version: 1, Interfaces: &interfaces})
	return string(data), err
}
