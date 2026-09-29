package main

import (
	"strings"
	"testing"
)

// vda 系统盘（vda1 /boot，vda2 /），vdb 挂 /data1，vdc 空盘，sr0 光驱。
const lsblkJSONSample = `{
  "blockdevices": [
    {"name":"sr0","kname":"sr0","type":"rom","fstype":null,"mountpoint":null,"size":"1024M","ro":"1"},
    {"name":"vda","kname":"vda","type":"disk","fstype":null,"mountpoint":null,"size":"100G","ro":"0",
      "children":[
        {"name":"vda1","kname":"vda1","type":"part","fstype":"xfs","mountpoint":"/boot","size":"1G","ro":"0"},
        {"name":"vda2","kname":"vda2","type":"part","fstype":"xfs","mountpoint":"/","size":"99G","ro":"0"}
      ]},
    {"name":"vdb","kname":"vdb","type":"disk","fstype":"xfs","mountpoint":"/data1","size":"500G","ro":"0"},
    {"name":"vdc","kname":"vdc","type":"disk","fstype":null,"mountpoint":null,"size":"500G","ro":false}
  ]
}`

const mountinfoSample = `22 1 253:2 / / rw,relatime shared:1 - xfs /dev/vda2 rw
23 22 253:1 / /boot rw,relatime shared:2 - xfs /dev/vda1 rw
24 22 253:16 / /data1 rw,relatime shared:3 - xfs /dev/vdb rw
25 22 0:22 / /proc rw,nosuid shared:4 - proc proc rw
26 22 253:16 / /data\040space rw,relatime shared:5 - xfs /dev/vdb rw`

func mustDevs(t *testing.T, s string) []blockDev {
	t.Helper()
	devs, err := parseLsblkJSON([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return devs
}

func TestSystemAndFreeDisks(t *testing.T) {
	devs := mustDevs(t, lsblkJSONSample)
	sys := systemDisks(devs)
	if !sys["vda"] || sys["vdb"] || sys["vdc"] {
		t.Fatalf("systemDisks = %v", sys)
	}
	free := freeDisks(devs, sys)
	if len(free) != 1 || free[0].KName != "vdc" {
		t.Fatalf("freeDisks = %+v，系统盘 vda 与已格式化 vdb 不应算空闲", free)
	}
	sum := diskSummary(devs)
	if !strings.Contains(sum, "vdc(500G)") || !strings.Contains(sum, "系统盘: vda") || !strings.Contains(sum, "vdb /data1") {
		t.Fatalf("diskSummary = %s", sum)
	}
}

func TestCheckDataRootRule1(t *testing.T) {
	devs := mustDevs(t, lsblkJSONSample)
	mounts := parseMountinfo(mountinfoSample)
	res := checkDataRoot("/data3", false, false, mounts, devs)
	if !res.OK || res.Rule != 1 || res.FreeDisk == nil || res.FreeDisk.KName != "vdc" {
		t.Fatalf("有空闲盘应通过并选 vdc: %+v", res)
	}
	noFree := strings.Replace(lsblkJSONSample, `"fstype":null,"mountpoint":null,"size":"500G","ro":false`, `"fstype":"xfs","mountpoint":null,"size":"500G","ro":false`, 1)
	res = checkDataRoot("/data3", false, false, mounts, mustDevs(t, noFree))
	if res.OK || res.Rule != 1 || !strings.Contains(res.Message, "没有未使用的磁盘") {
		t.Fatalf("无空闲盘应失败: %+v", res)
	}
}

func TestCheckDataRootRule2SystemDisk(t *testing.T) {
	devs := mustDevs(t, lsblkJSONSample)
	mounts := parseMountinfo(mountinfoSample)
	res := checkDataRoot("/data", true, true, mounts, devs)
	if res.OK || res.Rule != 2 || !strings.Contains(res.Message, "系统盘 vda") || !strings.Contains(res.Message, "vdc") {
		t.Fatalf("/data 在根分区应失败并提示空闲盘: %+v", res)
	}
}

func TestCheckDataRootRule2LVMSameDisk(t *testing.T) {
	lvm := `{"blockdevices":[
	  {"name":"sda","kname":"sda","type":"disk","size":"200G","ro":"0","children":[
	    {"name":"sda1","kname":"sda1","type":"part","fstype":"xfs","mountpoint":"/boot","size":"1G","ro":"0"},
	    {"name":"sda2","kname":"sda2","type":"part","fstype":"LVM2_member","size":"199G","ro":"0","children":[
	      {"name":"vg-root","kname":"dm-0","type":"lvm","fstype":"xfs","mountpoint":"/","size":"50G","ro":"0"},
	      {"name":"vg-data","kname":"dm-1","type":"lvm","fstype":"xfs","mountpoint":"/data","size":"149G","ro":"0"}
	    ]}
	  ]}
	]}`
	mounts := parseMountinfo(`1 0 253:0 / / rw - xfs /dev/mapper/vg-root rw
2 1 253:1 / /data rw - xfs /dev/mapper/vg-data rw`)
	res := checkDataRoot("/data", true, true, mounts, mustDevs(t, lvm))
	if res.OK || res.Rule != 2 {
		t.Fatalf("LVM data 卷与根卷同盘应算系统盘: %+v", res)
	}
}

func TestCheckDataRootRule3(t *testing.T) {
	devs := mustDevs(t, lsblkJSONSample)
	mounts := parseMountinfo(mountinfoSample)
	for _, p := range []string{"/data1", "/data1/be", "/data space/x"} {
		res := checkDataRoot(p, true, true, mounts, devs)
		if !res.OK || res.Rule != 3 || res.Disk != "vdb" {
			t.Fatalf("%s 在 vdb 上应直接使用: %+v", p, res)
		}
	}
}

func TestCheckDataRootInvalid(t *testing.T) {
	devs := mustDevs(t, lsblkJSONSample)
	mounts := parseMountinfo(mountinfoSample)
	if res := checkDataRoot("/", true, true, mounts, devs); res.OK {
		t.Fatal("/ 不允许")
	}
	if res := checkDataRoot("/data1/file", true, false, mounts, devs); res.OK {
		t.Fatal("非目录不允许")
	}
}

func TestParseLsblkPairs(t *testing.T) {
	text := `NAME="vda" KNAME="vda" PKNAME="" TYPE="disk" FSTYPE="" MOUNTPOINT="" SIZE="100G" RO="0"
NAME="vda1" KNAME="vda1" PKNAME="vda" TYPE="part" FSTYPE="xfs" MOUNTPOINT="/" SIZE="100G" RO="0"
NAME="vdc" KNAME="vdc" PKNAME="" TYPE="disk" FSTYPE="" MOUNTPOINT="" SIZE="500G" RO="0"`
	devs := parseLsblkPairs(text)
	sys := systemDisks(devs)
	free := freeDisks(devs, sys)
	if !sys["vda"] || len(free) != 1 || free[0].KName != "vdc" {
		t.Fatalf("pairs 解析错误: sys=%v free=%+v", sys, free)
	}
}

func TestFstabHasMountpoint(t *testing.T) {
	fstab := "# comment\nUUID=abc / xfs defaults 0 0\nUUID=def /data1 xfs defaults 0 0\n"
	if !fstabHasMountpoint(fstab, "/data1/") || fstabHasMountpoint(fstab, "/data3") {
		t.Fatal("fstab 判断错误")
	}
}
