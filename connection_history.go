package main

import (
	"encoding/json"

	"retty/internal/remote"
	"strings"
)

type desktopRecent struct {
	Link  string
	Name  string
	Alias string `json:",omitempty"`
	ID    string `json:",omitempty"`
	OS    string `json:",omitempty"`
}

func (d desktopRecent) displayName() string {
	if d.Alias != "" {
		return d.Alias
	}
	if d.Name != "" {
		return d.Name
	}
	return "桌面"
}

func sameDesktop(a, b desktopRecent) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	if a.Name == "桌面" || a.Name != b.Name {
		return false
	}
	if a.ID == "" || b.ID == "" {
		return true
	}
	// Upgrade records from early builds that used an installation UUID.
	if strings.HasPrefix(a.ID, "legacy:") || strings.HasPrefix(b.ID, "legacy:") {
		return strings.HasPrefix(a.ID, "machine:") || strings.HasPrefix(b.ID, "machine:")
	}
	return strings.HasPrefix(a.ID, "machine:") && !strings.Contains(b.ID, ":") ||
		strings.HasPrefix(b.ID, "machine:") && !strings.Contains(a.ID, ":")
}

func mergeDesktopHistory(first, second []desktopRecent) []desktopRecent {
	var history []desktopRecent
	for _, entries := range [][]desktopRecent{first, second} {
		for _, entry := range entries {
			if _, err := remote.ParseLink(entry.Link); err != nil {
				continue
			}
			if strings.TrimSpace(entry.Name) == "" {
				entry.Name = "桌面"
			}
			duplicate := false
			for i, kept := range history {
				if entry.Link == kept.Link || sameDesktop(entry, kept) {
					// Migrate older records without IDs while keeping the newest URL.
					if kept.ID == "" || strings.HasPrefix(entry.ID, "machine:") && !strings.HasPrefix(kept.ID, "legacy:") {
						history[i].ID = entry.ID
					}
					if kept.OS == "" {
						history[i].OS = entry.OS
					}
					if kept.Alias == "" {
						history[i].Alias = entry.Alias
					}
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			history = append(history, entry)
			if len(history) == 6 {
				return history
			}
		}
	}
	return history
}

// Both clients use the same history schema and merge rules. Persistence is a
// platform adapter: iOS uses MyGo SecureStore, desktop uses private app storage.
type connectionRecordStore interface {
	Get(string) ([]byte, error)
	Set(string, []byte) error
	Delete(string) error
}

func readDesktopHistory(store connectionRecordStore) []desktopRecent {
	if store == nil {
		return nil
	}
	var history []desktopRecent
	data, err := store.Get("history")
	if err == nil && json.Unmarshal(data, &history) == nil {
		return mergeDesktopHistory(history, nil)
	}
	if recent, err := store.Get("recent"); err == nil {
		history = []desktopRecent{{Link: string(recent), Name: "桌面"}}
	}
	return mergeDesktopHistory(history, nil)
}
func writeDesktopHistory(store connectionRecordStore, history []desktopRecent) error {
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	return store.Set("history", data)
}
func connectionStorage() chan func() {
	storage := make(chan func(), 32)
	go func() {
		for task := range storage {
			task()
		}
	}()
	return storage
}
