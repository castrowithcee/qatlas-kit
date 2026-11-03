package docker

import (
	"reflect"
	"testing"
)

func TestGroupSplitsProjectsAndLooseContainers(t *testing.T) {
	cs := []Container{
		{ID: "3", Name: "web-2", Labels: map[string]string{labelProject: "shop", labelWorkingDir: "/srv/shop", labelConfig: "/srv/shop/a.yml, /srv/shop/b.yml"}},
		{ID: "1", Name: "db", Labels: map[string]string{labelProject: "shop", labelWorkingDir: "/srv/shop"}},
		{ID: "2", Name: "solo"},
		{ID: "4", Name: "api", Labels: map[string]string{labelProject: "alpha"}},
		{ID: "5", Name: "web-1", Labels: map[string]string{labelProject: "shop", labelWorkingDir: "/srv/other"}},
	}
	projects, loose := Group(cs)
	if cs[0].ID != "3" {
		t.Fatal("Group changed its input")
	}
	if len(loose) != 1 || loose[0].Name != "solo" {
		t.Errorf("loose = %+v", loose)
	}
	var got []string
	for _, p := range projects {
		got = append(got, p.Name+"@"+p.WorkingDir)
	}
	if want := []string{"alpha@", "shop@/srv/other", "shop@/srv/shop"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	shop := projects[2]
	if shop.Containers[0].Name != "db" || shop.Containers[1].Name != "web-2" {
		t.Errorf("containers not sorted: %+v", shop.Containers)
	}
	if want := []string{"/srv/shop/a.yml", "/srv/shop/b.yml"}; !reflect.DeepEqual(shop.ConfigFiles, want) {
		t.Errorf("ConfigFiles = %v, want %v", shop.ConfigFiles, want)
	}
}

func TestUsesPathRespectsSeparatorBoundary(t *testing.T) {
	d := Details{
		Container: Container{Labels: map[string]string{labelWorkingDir: "/srv/apple/stack"}},
		Mounts:    []Mount{{Type: "bind", Source: "/srv/apple/data"}, {Type: "volume", Source: "/var/lib/vol/x"}, {Type: "tmpfs"}},
	}
	tests := []struct {
		dir  string
		want bool
	}{
		{"/srv/apple", true},
		{"/srv/apple/", true},
		{"/srv/apple/data", true},
		{"/srv/apple/data/../data", true},
		{"/srv/app", false},
		{"/srv/apple/dat", false},
		{"/srv/apple/data/sub", false},
		{"/", true},
		{"srv/apple", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := d.UsesPath(tt.dir); got != tt.want {
			t.Errorf("UsesPath(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
	if (Details{Mounts: []Mount{{Source: "relative/x"}}}).UsesPath("/") {
		t.Error("a relative mount source matched")
	}
}

func TestStatsComputation(t *testing.T) {
	var a statsAnswer
	a.CPU.CPUUsage.TotalUsage, a.PreCPU.CPUUsage.TotalUsage = 300, 100
	a.CPU.SystemCPUUsage, a.PreCPU.SystemCPUUsage = 1200, 200
	a.CPU.OnlineCPUs = 4
	a.Memory.Usage, a.Memory.Limit = 1000, 4000
	v1, v2 := uint64(100), uint64(300)
	a.Memory.Stats.TotalInactiveFile = &v1
	if s := a.stats(); s.CPUPercent != 80 || s.MemoryUsed != 900 || s.MemoryLimit != 4000 {
		t.Errorf("v1: %+v", s)
	}
	a.Memory.Stats.InactiveFile = &v2
	if s := a.stats(); s.MemoryUsed != 700 {
		t.Errorf("v2 must win: %+v", s)
	}
	v3 := uint64(5000)
	a.Memory.Stats.InactiveFile = &v3
	a.CPU.OnlineCPUs = 0
	a.CPU.CPUUsage.PercpuUsage = []uint64{1, 2}
	if s := a.stats(); s.MemoryUsed != 0 || s.CPUPercent != 40 {
		t.Errorf("fallback: %+v", s)
	}
	if s := (statsAnswer{}).stats(); s != (Stats{}) {
		t.Errorf("zero answer: %+v", s)
	}
}
