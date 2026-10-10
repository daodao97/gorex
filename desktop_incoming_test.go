package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
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
	old.DeviceID = strings.Repeat("a", 32)
	latest.ID, latest.DeviceID = "reconnected-phone", old.DeviceID
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

func TestIncomingDeviceHistoryDeduplicatesReconnects(t *testing.T) {
	now := time.Now()
	phone := strings.Repeat("a", 32)
	other := strings.Repeat("b", 32)
	for _, tc := range []struct {
		name    string
		devices []remote.ConnectedDevice
		want    int
	}{
		{"legacy reconnects", []remote.ConnectedDevice{
			{ID: "old", Name: "iPhone", OS: "iOS 26", Connected: now.Add(-time.Hour)},
			{ID: "latest", Name: "iPhone", OS: "iOS 26", Connected: now},
		}, 1},
		{"stable identity survives rename and OS update", []remote.ConnectedDevice{
			{ID: "old", DeviceID: phone, Name: "Old phone", OS: "iOS 25", Connected: now.Add(-time.Hour)},
			{ID: "latest", DeviceID: phone, Name: "iPhone", OS: "iOS 26", Connected: now},
		}, 1},
		{"migrate legacy history", []remote.ConnectedDevice{
			{ID: "old", Name: "iPhone", OS: "iOS 26", Connected: now.Add(-time.Hour)},
			{ID: "latest", DeviceID: phone, Name: "iPhone", OS: "iOS 26", Connected: now},
		}, 1},
		{"distinct phones with identical names", []remote.ConnectedDevice{
			{ID: "other", DeviceID: other, Name: "iPhone", OS: "iOS 26", Connected: now.Add(-time.Hour)},
			{ID: "latest", DeviceID: phone, Name: "iPhone", OS: "iOS 26", Connected: now},
		}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeIncomingDevices(tc.devices[:1], tc.devices[1:])
			if len(got) != tc.want || got[0].ID != "latest" || !got[0].Connected.Equal(now) {
				t.Fatalf("unexpected device history: %+v", got)
			}
		})
	}
}
