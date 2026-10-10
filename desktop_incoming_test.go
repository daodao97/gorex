package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"retty/internal/remote"
)

func TestIncomingDevicesPersistAcrossDesktopReopen(t *testing.T) {
	a, tt := newStaticTestApp(t)
	store := &desktopConnectionStore{path: filepath.Join(t.TempDir(), "connections.json")}
	a.desktops.store, a.desktops.historyLoaded = store, true
	a.desktopStorage = connectionStorage()
	t.Cleanup(func() { close(a.desktopStorage) })
	old := remote.ConnectedDevice{ID: "phone", Name: "Old phone name", OS: "iOS", Connected: time.Now().Add(-time.Hour)}
	latest := old
	latest.Name, latest.Connected = "我的 iPhone", time.Now()
	a.desktops.incoming = mergeIncomingDevices([]remote.ConnectedDevice{latest}, []remote.ConnectedDevice{old})
	a.saveDesktopHistory()
	a.flushDesktopHistory()
	data, err := store.Get("incoming-devices")
	if err != nil {
		t.Fatal(err)
	}
	var records []remote.ConnectedDevice
	if err := json.Unmarshal(data, &records); err != nil || len(records) != 1 || records[0].Name != latest.Name {
		t.Fatal("recent device record did not persist newest metadata", err)
	}
	a.desktops.incoming = mergeIncomingDevices(nil, records)
	a.showDesktopConnections(nil)
	tt.Frame()
	if !tt.HasText("我的 iPhone") || !tt.HasText("最近接入此电脑") || !tt.HasText("离线") {
		t.Fatal("saved device missing or falsely shown as connected")
	}
	if _, ok := tt.Find("断开设备 我的 iPhone"); ok {
		t.Fatal("offline device offered a live disconnect action")
	}
}
