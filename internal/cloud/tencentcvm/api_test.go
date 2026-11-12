package tencentcvm

import "testing"

func TestMapInstanceSystemDisk(t *testing.T) {
	dataOnly := map[string]any{"InstanceId": "ins-none", "DataDisks": []any{map[string]any{"DiskSize": float64(900)}}}
	fillSystemDisk(dataOnly, sysDisk{})
	if got := mapInstance(dataOnly, "ap-beijing").DiskGB; got != 900 {
		t.Fatalf("data only disk=%d", got)
	}

	local := map[string]any{
		"InstanceId": "ins-local",
		"DataDisks":  []any{map[string]any{"DiskSize": float64(1000)}},
		"SystemDisk": map[string]any{"DiskSize": float64(100), "DiskType": "LOCAL_BASIC"},
	}
	fillSystemDisk(local, sysDisk{Size: 80, Type: "CLOUD_SSD"})
	if got := mapInstance(local, "ap-beijing").DiskGB; got != 1100 {
		t.Fatalf("existing system disk counted twice: %d", got)
	}

	cbs := map[string]any{
		"InstanceId": "ins-cbs",
		"DataDisks":  []any{map[string]any{"DiskSize": float64(1500)}},
		"SystemDisk": map[string]any{"DiskId": "disk-sys"},
	}
	fillSystemDisk(cbs, sysDisk{Size: 50, Type: "CLOUD_PREMIUM"})
	if got := mapInstance(cbs, "ap-beijing").DiskGB; got != 1550 {
		t.Fatalf("cbs system disk disk=%d", got)
	}
	sys := cbs["SystemDisk"].(map[string]any)
	if sys["DiskType"] != "CLOUD_PREMIUM" {
		t.Fatalf("system disk type %v", sys["DiskType"])
	}
}

func TestMergeDataDisks(t *testing.T) {
	two := map[string]any{"InstanceId": "ins-1", "DataDisks": []any{
		map[string]any{"DiskId": "disk-a", "DiskSize": float64(0)},
		map[string]any{"DiskId": "disk-b"},
	}}
	mergeDataDisks(two, []dataDisk{{ID: "disk-a", Size: 500, Type: "CLOUD_SSD"}, {ID: "disk-b", Size: 400, Type: "CLOUD_PREMIUM"}})
	if got := mapInstance(two, "ap-beijing").DiskGB; got != 900 {
		t.Fatalf("two cloud disks disk=%d", got)
	}

	same := map[string]any{"InstanceId": "ins-2", "DataDisks": []any{
		map[string]any{"DiskId": "disk-a", "DiskSize": float64(500)},
	}}
	mergeDataDisks(same, []dataDisk{{ID: "disk-a", Size: 500, Type: "CLOUD_SSD"}})
	if got := mapInstance(same, "ap-beijing").DiskGB; got != 500 {
		t.Fatalf("same disk id counted twice: %d", got)
	}

	mixed := map[string]any{"InstanceId": "ins-3", "DataDisks": []any{
		map[string]any{"DiskSize": float64(200), "DiskType": "LOCAL_SSD"},
	}}
	mergeDataDisks(mixed, []dataDisk{{ID: "disk-c", Size: 500, Type: "CLOUD_SSD"}})
	if got := mapInstance(mixed, "ap-beijing").DiskGB; got != 700 {
		t.Fatalf("local plus cloud disk=%d", got)
	}

	keep := map[string]any{"InstanceId": "ins-4", "DataDisks": []any{
		map[string]any{"DiskSize": float64(300)},
		map[string]any{"DiskSize": float64(600)},
	}}
	mergeDataDisks(keep, nil)
	if got := mapInstance(keep, "ap-beijing").DiskGB; got != 900 {
		t.Fatalf("kept cvm disks disk=%d", got)
	}
}
