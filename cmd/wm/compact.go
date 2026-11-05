package main

import "strings"

func compactName(w workload) string {
	parts := strings.Split(w.Address, ".")
	var path []string
	i := 0
	for i+1 < len(parts) && parts[i] == "module" {
		path = append(path, parts[i+1])
		i += 2
	}
	if len(path) < 2 || i+1 >= len(parts) {
		return w.Name
	}
	resource := parts[i+1]
	if resource != "service" && resource != path[len(path)-1] {
		resource = strings.TrimPrefix(resource, path[len(path)-1]+"_")
		return strings.Join(path, "/") + ":" + strings.ReplaceAll(resource, "_", "-")
	}
	return strings.Join(path, "/")
}
func compactVersion(w workload) string {
	version := w.Version
	if w.Kind == "model" {
		if version == "" || version == "—" {
			version = w.Engine
		}
		if w.HFModel != "" {
			model := w.HFModel
			if slash := strings.LastIndex(model, "/"); slash >= 0 {
				model = model[slash+1:]
			}
			revision := w.HFRevision
			if len(revision) > 8 {
				revision = revision[:8]
			}
			if revision != "" {
				model += "@" + revision
			}
			version += " · " + model
		}
	}
	return version
}
func compactStatus(w workload) string {
	if w.Error != "" {
		return "unknown"
	}
	if w.Status == "running" && w.Health != "" && w.Health != "—" {
		return w.Health
	}
	if w.Status == "exited" || w.Status == "created" {
		return "stopped"
	}
	return w.Status
}
