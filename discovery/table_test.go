package discovery

import (
	"testing"
	"time"

	"github.com/megawron/lok8s/types"
)

func TestConvertPodsToTable(t *testing.T) {
	now := time.Now().UTC()
	pods := []types.Pod{
		{
			Metadata: types.ObjectMeta{
				Name:              "pod-1",
				Namespace:         "default",
				CreationTimestamp: now.Add(-10 * time.Minute),
			},
			Status: types.PodStatus{
				Phase:    types.PodRunning,
				HostPort: 8080,
				ContainerStatuses: []types.ContainerStatus{
					{
						Ready:        true,
						RestartCount: 2,
					},
				},
			},
		},
		{
			Metadata: types.ObjectMeta{
				Name:              "pod-2",
				Namespace:         "default",
				CreationTimestamp: now.Add(-50 * time.Second),
			},
			Status: types.PodStatus{
				Phase:        types.PodPending,
				RestartCount: 0,
			},
		},
	}

	table := ConvertPodsToTable(pods)

	if table.Kind != "Table" {
		t.Errorf("Expected Kind 'Table', got %q", table.Kind)
	}

	if len(table.ColumnDefinitions) != 6 {
		t.Errorf("Expected 6 columns, got %d", len(table.ColumnDefinitions))
	}

	if len(table.Rows) != 2 {
		t.Fatalf("Expected 2 rows, got %d", len(table.Rows))
	}

	// Verify pod-1 row cells: Name, Ready, Status, Port, Restarts, Age
	row1 := table.Rows[0]
	if row1.Cells[0] != "pod-1" {
		t.Errorf("Expected cell 0 to be 'pod-1', got %v", row1.Cells[0])
	}
	if row1.Cells[1] != "1/1" {
		t.Errorf("Expected cell 1 to be '1/1', got %v", row1.Cells[1])
	}
	if row1.Cells[2] != "Running" {
		t.Errorf("Expected cell 2 to be 'Running', got %v", row1.Cells[2])
	}
	if row1.Cells[3] != "8080" {
		t.Errorf("Expected cell 3 to be '8080', got %v", row1.Cells[3])
	}
	if row1.Cells[4] != "2" {
		t.Errorf("Expected cell 4 to be '2', got %v", row1.Cells[4])
	}
	if row1.Cells[5] != "10m" {
		t.Errorf("Expected cell 5 to be '10m', got %v", row1.Cells[5])
	}

	// Verify pod-2 row cells: Name, Ready, Status, Port, Restarts, Age
	row2 := table.Rows[1]
	if row2.Cells[0] != "pod-2" {
		t.Errorf("Expected cell 0 to be 'pod-2', got %v", row2.Cells[0])
	}
	if row2.Cells[1] != "0/1" {
		t.Errorf("Expected cell 1 to be '0/1', got %v", row2.Cells[1])
	}
	if row2.Cells[2] != "Pending" {
		t.Errorf("Expected cell 2 to be 'Pending', got %v", row2.Cells[2])
	}
	if row2.Cells[3] != "<none>" {
		t.Errorf("Expected cell 3 to be '<none>', got %v", row2.Cells[3])
	}
	if row2.Cells[4] != "0" {
		t.Errorf("Expected cell 4 to be '0', got %v", row2.Cells[4])
	}
	if row2.Cells[5] != "50s" {
		t.Errorf("Expected cell 5 to be '50s', got %v", row2.Cells[5])
	}
}

func TestTranslateTimestamp(t *testing.T) {
	now := time.Now()
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{500 * time.Millisecond, "0s"},
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := translateTimestamp(now.Add(-tt.duration))
			if got != tt.want {
				t.Errorf("translateTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}
