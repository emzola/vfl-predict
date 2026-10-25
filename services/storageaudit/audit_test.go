package storageaudit

import "testing"

func TestFormatBytesUsesBinaryUnits(t *testing.T) {
	if got := FormatBytes(0); got != "0 B" {
		t.Fatalf("zero bytes = %q", got)
	}
	if got := FormatBytes(1024 * 1024); got != "1.00 MiB" {
		t.Fatalf("one MiB = %q", got)
	}
}

func TestCapacityAlertsWarnBeforeFilesystemIsFull(t *testing.T) {
	alerts := CapacityAlerts(100, 20, 3*1024*1024*1024, 11*1024*1024*1024)
	if len(alerts) != 3 {
		t.Fatalf("alerts = %#v", alerts)
	}
	if alerts[0].Level != "warning" || alerts[0].Metric != "filesystem_free_percent" {
		t.Fatalf("filesystem alert = %#v", alerts[0])
	}
}

func TestCapacityAlertsIgnoreUnmeasuredFilesystem(t *testing.T) {
	if alerts := CapacityAlerts(0, 0, 0, 0); len(alerts) != 0 {
		t.Fatalf("unmeasured alerts = %#v", alerts)
	}
}
