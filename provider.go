package anet

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// InterfaceInfo is one interface and its unicast addresses from one snapshot.
// Callers must use InterfaceAddrsByInterface rather than net.Interface.Addrs:
// net.Interface's methods do not dispatch through this package's provider.
type InterfaceInfo struct {
	Interface net.Interface
	Addresses []net.Addr
}

// InterfaceProvider supplies a complete current snapshot, including an empty
// snapshot when offline. Implementations must support concurrent calls, return
// promptly, and keep their returned data immutable during the call. Errors are
// returned to the caller; they never cause a fallback to system enumeration.
type InterfaceProvider interface {
	Snapshot() ([]InterfaceInfo, error)
}

var registry struct {
	sync.RWMutex
	provider InterfaceProvider
}

var errNoSuchInterface = errors.New("no such network interface")
var errInvalidInterface = errors.New("invalid network interface")

// SetInterfaceProvider installs a process-wide provider. Register at application
// startup, not per Runtime or Activity. nil restores the platform default.
// Registration does not close the previous provider. An in-flight query keeps
// its selected provider; callbacks execute outside the registration lock.
func SetInterfaceProvider(provider InterfaceProvider) {
	registry.Lock()
	registry.provider = provider
	registry.Unlock()
}

func currentProvider() InterfaceProvider {
	registry.RLock()
	provider := registry.provider
	registry.RUnlock()
	return provider
}

func routeError(err error) error {
	return &net.OpError{Op: "route", Net: "ip+net", Err: err}
}

func providerSnapshot(provider InterfaceProvider) ([]InterfaceInfo, error) {
	data, err := provider.Snapshot()
	if err != nil {
		return nil, routeError(err)
	}
	out := make([]InterfaceInfo, 0, len(data))
	indices, names := map[int]bool{}, map[string]bool{}
	for _, item := range data {
		iface := item.Interface
		if iface.Index <= 0 || iface.Name == "" || iface.MTU < 0 || indices[iface.Index] || names[iface.Name] {
			return nil, routeError(errors.New("invalid interface provider snapshot"))
		}
		indices[iface.Index], names[iface.Name] = true, true
		iface.HardwareAddr = append(net.HardwareAddr(nil), iface.HardwareAddr...)
		copy := InterfaceInfo{Interface: iface, Addresses: make([]net.Addr, 0, len(item.Addresses))}
		for _, address := range item.Addresses {
			switch address := address.(type) {
			case *net.IPNet:
				if address == nil || address.IP.To16() == nil {
					return nil, routeError(errors.New("invalid interface provider address"))
				}
				_, bits := address.Mask.Size()
				if (bits != 32 && bits != 128) || (bits == 32 && address.IP.To4() == nil) {
					return nil, routeError(errors.New("invalid interface provider prefix"))
				}
				copy.Addresses = append(copy.Addresses, &net.IPNet{IP: append(net.IP(nil), address.IP...), Mask: append(net.IPMask(nil), address.Mask...)})
			case *net.IPAddr:
				if address == nil || address.IP.To16() == nil {
					return nil, routeError(errors.New("invalid interface provider address"))
				}
				copy.Addresses = append(copy.Addresses, &net.IPAddr{IP: append(net.IP(nil), address.IP...), Zone: address.Zone})
			default:
				return nil, routeError(fmt.Errorf("unsupported interface provider address type %T", address))
			}
		}
		out = append(out, copy)
	}
	return out, nil
}

// Snapshot returns interfaces and addresses from one provider call. Consumers
// needing both should prefer this over multiple independently timed queries.
func Snapshot() ([]InterfaceInfo, error) {
	if provider := currentProvider(); provider != nil {
		return providerSnapshot(provider)
	}
	interfaces, err := systemInterfaces()
	if err != nil {
		return nil, err
	}
	out := make([]InterfaceInfo, 0, len(interfaces))
	for _, iface := range interfaces {
		addresses, err := systemInterfaceAddrsByInterface(&iface)
		if err != nil {
			return nil, err
		}
		out = append(out, InterfaceInfo{Interface: iface, Addresses: addresses})
	}
	return out, nil
}

// Interfaces returns system interfaces or the registered provider's interfaces.
func Interfaces() ([]net.Interface, error) {
	provider := currentProvider()
	if provider == nil {
		return systemInterfaces()
	}
	snapshot, err := providerSnapshot(provider)
	if err != nil {
		return nil, err
	}
	out := make([]net.Interface, 0, len(snapshot))
	for _, item := range snapshot {
		out = append(out, item.Interface)
	}
	return out, nil
}

// InterfaceAddrs returns unicast addresses from all visible interfaces.
func InterfaceAddrs() ([]net.Addr, error) {
	provider := currentProvider()
	if provider == nil {
		return systemInterfaceAddrs()
	}
	snapshot, err := providerSnapshot(provider)
	if err != nil {
		return nil, err
	}
	out := make([]net.Addr, 0)
	for _, item := range snapshot {
		out = append(out, item.Addresses...)
	}
	return out, nil
}

// InterfaceByName looks up a visible interface by its exact name.
func InterfaceByName(name string) (*net.Interface, error) {
	if name == "" {
		return nil, routeError(errInvalidInterface)
	}
	interfaces, err := Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		if iface.Name == name {
			return &iface, nil
		}
	}
	return nil, routeError(errNoSuchInterface)
}

// InterfaceByIndex looks up a visible interface by its positive system index.
func InterfaceByIndex(index int) (*net.Interface, error) {
	if index <= 0 {
		return nil, routeError(errInvalidInterface)
	}
	interfaces, err := Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		if iface.Index == index {
			return &iface, nil
		}
	}
	return nil, routeError(errNoSuchInterface)
}

// InterfaceAddrsByInterface queries the current provider, never the methods on
// net.Interface. A changed name at a reused index rejects a retired interface.
func InterfaceAddrsByInterface(iface *net.Interface) ([]net.Addr, error) {
	if iface == nil || iface.Index <= 0 {
		return nil, routeError(errInvalidInterface)
	}
	provider := currentProvider()
	if provider == nil {
		return systemInterfaceAddrsByInterface(iface)
	}
	snapshot, err := providerSnapshot(provider)
	if err != nil {
		return nil, err
	}
	for _, item := range snapshot {
		if item.Interface.Index == iface.Index && (iface.Name == "" || item.Interface.Name == iface.Name) {
			return item.Addresses, nil
		}
	}
	return nil, routeError(errNoSuchInterface)
}
