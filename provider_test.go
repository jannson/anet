package anet_test

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jannson/anet"
)

type providerFunc func() ([]anet.InterfaceInfo, error)

func (f providerFunc) Snapshot() ([]anet.InterfaceInfo, error) { return f() }

func records(ip string) []anet.InterfaceInfo {
	address, network, _ := net.ParseCIDR(ip)
	network.IP = address
	return []anet.InterfaceInfo{{Interface: net.Interface{Index: 42, Name: "native-wifi", MTU: 1500,
		Flags: net.FlagUp | net.FlagMulticast, HardwareAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6}},
		Addresses: []net.Addr{network}}}
}

func install(t *testing.T, p anet.InterfaceProvider) {
	t.Helper()
	anet.SetInterfaceProvider(p)
	t.Cleanup(func() { anet.SetInterfaceProvider(nil) })
}

func TestProviderOwnsAllInterfaceQueries(t *testing.T) {
	install(t, providerFunc(func() ([]anet.InterfaceInfo, error) { return records("192.0.2.12/24"), nil }))
	interfaces, err := anet.Interfaces()
	if err != nil || len(interfaces) != 1 || interfaces[0].Name != "native-wifi" {
		t.Fatalf("interfaces=%v err=%v", interfaces, err)
	}
	byName, err := anet.InterfaceByName("native-wifi")
	if err != nil || byName.Index != 42 {
		t.Fatalf("by name=%v err=%v", byName, err)
	}
	byIndex, err := anet.InterfaceByIndex(42)
	if err != nil || byIndex.Name != "native-wifi" {
		t.Fatalf("by index=%v err=%v", byIndex, err)
	}
	for _, query := range []func() ([]net.Addr, error){anet.InterfaceAddrs, func() ([]net.Addr, error) {
		return anet.InterfaceAddrsByInterface(byName)
	}} {
		addresses, err := query()
		if err != nil || len(addresses) != 1 || addresses[0].String() != "192.0.2.12/24" {
			t.Fatalf("addresses=%v err=%v", addresses, err)
		}
	}
	if _, err := anet.InterfaceAddrsByInterface(&net.Interface{Index: 42, Name: "retired-wifi"}); err == nil {
		t.Fatal("reused index accepted a retired interface identity")
	}
}

func TestProviderErrorDoesNotFallBack(t *testing.T) {
	want := errors.New("native query denied")
	install(t, providerFunc(func() ([]anet.InterfaceInfo, error) { return nil, want }))
	_, first := anet.Interfaces()
	_, second := anet.InterfaceAddrs()
	_, third := anet.InterfaceByName("native-wifi")
	_, fourth := anet.InterfaceByIndex(42)
	_, fifth := anet.InterfaceAddrsByInterface(&net.Interface{Index: 42})
	for _, err := range []error{first, second, third, fourth, fifth} {
		if !errors.Is(err, want) {
			t.Fatalf("provider error hidden by fallback: %v", err)
		}
	}
}

func TestProviderSnapshotChangesAndEmptyAreVisible(t *testing.T) {
	var current atomic.Value
	current.Store(records("192.0.2.12/24"))
	install(t, providerFunc(func() ([]anet.InterfaceInfo, error) { return current.Load().([]anet.InterfaceInfo), nil }))
	for _, cidr := range []string{"192.0.2.12/24", "198.51.100.12/24"} {
		current.Store(records(cidr))
		addresses, err := anet.InterfaceAddrs()
		if err != nil || len(addresses) != 1 || addresses[0].String() != cidr {
			t.Fatalf("stale addresses=%v err=%v", addresses, err)
		}
	}
	current.Store([]anet.InterfaceInfo{})
	if list, err := anet.Interfaces(); err != nil || len(list) != 0 {
		t.Fatalf("offline snapshot must stay empty: %v %v", list, err)
	}
}

func TestProviderCannotBeMutatedThroughResults(t *testing.T) {
	data := records("192.0.2.12/24")
	install(t, providerFunc(func() ([]anet.InterfaceInfo, error) { return data, nil }))
	list, _ := anet.Interfaces()
	list[0].HardwareAddr[0] = 255
	addresses, _ := anet.InterfaceAddrs()
	addresses[0].(*net.IPNet).IP[0] = 255
	addresses[0].(*net.IPNet).Mask[0] = 0
	if data[0].Interface.HardwareAddr[0] != 1 || data[0].Addresses[0].String() != "192.0.2.12/24" {
		t.Fatal("query exposed provider-owned mutable state")
	}
}

func TestProviderCallbackRunsOutsideRegistrationLock(t *testing.T) {
	install(t, providerFunc(func() ([]anet.InterfaceInfo, error) {
		anet.SetInterfaceProvider(nil)
		return records("192.0.2.12/24"), nil
	}))
	done := make(chan error, 1)
	go func() { _, err := anet.Interfaces(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("callback deadlocked registration")
	}
}

func TestConcurrentRegistrationAndQueries(t *testing.T) {
	p := providerFunc(func() ([]anet.InterfaceInfo, error) { return records("192.0.2.12/24"), nil })
	install(t, p)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				anet.SetInterfaceProvider(p)
				if list, err := anet.Interfaces(); err != nil || len(list) != 1 {
					t.Errorf("query=%v err=%v", list, err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestInvalidProviderSnapshotIsRejected(t *testing.T) {
	for _, data := range [][]anet.InterfaceInfo{
		{{Interface: net.Interface{Name: "native-wifi"}}},
		{{Interface: net.Interface{Index: 42}}},
		{records("192.0.2.12/24")[0], records("192.0.2.13/24")[0]},
		{{Interface: net.Interface{Index: 42, Name: "native-wifi"}, Addresses: []net.Addr{nil}}},
	} {
		install(t, providerFunc(func() ([]anet.InterfaceInfo, error) { return data, nil }))
		if _, err := anet.Interfaces(); err == nil {
			t.Fatal("invalid provider snapshot accepted")
		}
	}
}

func TestDefaultProviderRemainsAvailable(t *testing.T) {
	anet.SetInterfaceProvider(nil)
	list, err := anet.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range list {
		byName, err := anet.InterfaceByName(iface.Name)
		if err != nil || byName.Index != iface.Index {
			t.Fatalf("default lookup=%v err=%v", byName, err)
		}
		if _, err := anet.InterfaceAddrsByInterface(&iface); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := anet.InterfaceAddrsByInterface(nil); err == nil {
		t.Fatal("nil interface accepted")
	}
}
