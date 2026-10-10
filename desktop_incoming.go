package main

import (
	"cmp"
	"retty/internal/remote"
	"slices"
	"strings"
)

func mergeIncomingDevices(first, second []remote.ConnectedDevice) []remote.ConnectedDevice {
	var records []remote.ConnectedDevice
	seen := map[string]bool{}
	all := append(slices.Clone(first), second...)
	// Legacy records have only a transient peer ID. Associate their metadata
	// with an installation only when it identifies a single known device.
	identities := map[string]string{}
	for _, d := range all {
		if d.DeviceID == "" || d.ID == "" {
			continue
		}
		key := incomingDeviceMetadata(d)
		if id, exists := identities[key]; !exists {
			identities[key] = d.DeviceID
		} else if id != d.DeviceID {
			identities[key] = ""
		}
	}
	slices.SortStableFunc(all, func(a, b remote.ConnectedDevice) int {
		return cmp.Compare(b.Connected.UnixNano(), a.Connected.UnixNano())
	})
	for _, d := range all {
		if d.ID == "" {
			continue
		}
		key := "device:" + d.DeviceID
		if d.DeviceID == "" {
			metadata := incomingDeviceMetadata(d)
			if id := identities[metadata]; id != "" {
				d.DeviceID, key = id, "device:"+id
			} else {
				key = "legacy:" + metadata
			}
		}
		if seen[key] || seen["peer:"+d.ID] {
			continue
		}
		seen[key], seen["peer:"+d.ID] = true, true
		records = append(records, d)
		if len(records) == 12 {
			break
		}
	}
	return records
}

func incomingDeviceMetadata(d remote.ConnectedDevice) string {
	name, os := strings.TrimSpace(d.Name), strings.TrimSpace(d.OS)
	if name == "" {
		return d.ID
	}
	return name + "\x00" + os
}
