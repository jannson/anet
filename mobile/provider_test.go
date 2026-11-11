package mobile_test

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/jannson/anet"
	"github.com/jannson/anet/mobile"
)

type callback struct {
	payload string
	err     error
}

func (p callback) SnapshotJSON() (string, error) { return p.payload, p.err }

func TestNativeCallbackFeedsEveryTypedQuery(t *testing.T) {
	mobile.SetInterfaceProvider(callback{payload: `{"version":1,"interfaces":[{"index":42,"name":"native-wifi","mtu":1500,"up":true,"multicast":true,"addresses":["192.0.2.12/24","fe80::12/64"]}]}`})
	t.Cleanup(func() { mobile.SetInterfaceProvider(nil) })
	iface, err := anet.InterfaceByName("native-wifi")
	if err != nil || iface.Index != 42 || iface.Flags&net.FlagMulticast == 0 {
		t.Fatalf("interface=%v err=%v", iface, err)
	}
	addresses, err := anet.InterfaceAddrsByInterface(iface)
	if err != nil || len(addresses) != 2 || addresses[0].String() != "192.0.2.12/24" || addresses[1].String() != "fe80::12/64" {
		t.Fatalf("addresses=%v err=%v", addresses, err)
	}
	encoded, err := mobile.InterfaceSnapshotJSON()
	if err != nil || !strings.Contains(encoded, "192.0.2.12/24") {
		t.Fatalf("round trip=%s err=%v", encoded, err)
	}
}

func TestNativeMalformedOrFailedSnapshotsNeverFallBack(t *testing.T) {
	t.Cleanup(func() { mobile.SetInterfaceProvider(nil) })
	for _, payload := range []string{`{`, `{}`, `{"version":2,"interfaces":[]}`, `{"version":1,"interfaces":null}`,
		`{"version":1,"interfaces":[{"index":42,"name":"wifi","addresses":["192.0.2.12/33"]}]}`,
		`{"version":1,"interfaces":[{"index":0,"name":"wifi"}]}`, strings.Repeat("x", (1<<20)+1)} {
		mobile.SetInterfaceProvider(callback{payload: payload})
		if _, err := anet.Interfaces(); err == nil {
			t.Fatal("invalid native snapshot accepted")
		}
	}
	want := errors.New("native callback failed")
	mobile.SetInterfaceProvider(callback{err: want})
	if _, err := anet.InterfaceAddrs(); !errors.Is(err, want) {
		t.Fatalf("native error lost: %v", err)
	}
	mobile.SetInterfaceProvider(callback{payload: `{"version":1,"interfaces":[]}`})
	if list, err := anet.Interfaces(); err != nil || len(list) != 0 {
		t.Fatalf("offline must stay empty: %v %v", list, err)
	}
}
