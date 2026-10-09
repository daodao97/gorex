package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"retty/internal/rex"
	"sync"
)

// MyGo SecureStore currently supports iOS only. The desktop adapter follows
// the existing Tailcat identity's private file permissions and atomic writes.
// Link/history policy stays shared; this is only the persistence backend.
type desktopConnectionStore struct {
	path string
	mu   sync.Mutex
}

func newDesktopConnectionStore() connectionRecordStore {
	return &desktopConnectionStore{path: filepath.Join(rex.Dir(), "desktop-connections.json")}
}
func (s *desktopConnectionStore) records() (map[string]json.RawMessage, error) {
	records := map[string]json.RawMessage{}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}
func (s *desktopConnectionStore) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.records()
	if err != nil {
		return nil, err
	}
	data, ok := records[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (s *desktopConnectionStore) Set(key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.records()
	if err != nil {
		return err
	}
	records[key] = append(json.RawMessage(nil), data...)
	return s.write(records)
}
func (s *desktopConnectionStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.records()
	if err != nil {
		return err
	}
	delete(records, key)
	return s.write(records)
}
func (s *desktopConnectionStore) write(records map[string]json.RawMessage) error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".connections-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
