//go:build !android
// +build !android

package anet

import (
	"net"
)

// Interfaces returns a list of the system's network interfaces.
func systemInterfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

// InterfaceAddrs returns a list of the system's unicast interface
// addresses.
//
// The returned list does not identify the associated interface; use
// Interfaces and Interface.Addrs for more detail.
func systemInterfaceAddrs() ([]net.Addr, error) {
	return net.InterfaceAddrs()
}

// InterfaceAddrsByInterface returns a list of the system's unicast
// interface addresses by specific interface.
func systemInterfaceAddrsByInterface(ifi *net.Interface) ([]net.Addr, error) {
	return ifi.Addrs()
}

func SetAndroidVersion(version uint) {}
