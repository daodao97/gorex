package rex

import "testing"

func TestDeviceIdentitySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := loadDeviceID(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadDeviceID(dir)
	if err != nil || first != second {
		t.Fatalf("identity changed: %v", err)
	}
	other, err := loadDeviceID(t.TempDir())
	if err != nil || first == other {
		t.Fatalf("different desktops share identity: %v", err)
	}
}
