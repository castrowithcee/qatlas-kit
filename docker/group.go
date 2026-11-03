package docker

import (
	"sort"
	"strings"
)

// Project is the set of containers of one Compose project, taken from their labels.
type Project struct {
	Name        string
	WorkingDir  string
	ConfigFiles []string
	Containers  []Container
}

// Group splits cs by the Compose project label. Containers with the same project name and working directory
// form one Project, whose config files are those of its first container (by name) that has any. Containers
// without a project label are returned as loose. Both results are sorted, and cs is not changed.
func Group(cs []Container) (projects []Project, loose []Container) {
	type key struct{ name, dir string }
	index := map[key]int{}
	sorted := append([]Container(nil), cs...)
	sortContainers(sorted)
	for _, c := range sorted {
		name := c.Labels[labelProject]
		if name == "" {
			loose = append(loose, c)
			continue
		}
		k := key{name, c.Labels[labelWorkingDir]}
		i, ok := index[k]
		if !ok {
			i = len(projects)
			index[k] = i
			projects = append(projects, Project{Name: name, WorkingDir: k.dir})
		}
		if projects[i].ConfigFiles == nil {
			projects[i].ConfigFiles = splitList(c.Labels[labelConfig])
		}
		projects[i].Containers = append(projects[i].Containers, c)
	}
	sort.SliceStable(projects, func(i, j int) bool {
		if projects[i].Name != projects[j].Name {
			return projects[i].Name < projects[j].Name
		}
		return projects[i].WorkingDir < projects[j].WorkingDir
	})
	return projects, loose
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
