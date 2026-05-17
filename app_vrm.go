package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VRMModelInfo describes a VRM model available to the frontend.
type VRMModelInfo struct {
	Name   string `json:"name"`
	URL    string `json:"url"`    // asset URL usable by the frontend
	Source string `json:"source"` // "builtin" | "user"
	SizeKB int    `json:"size_kb"`
}

// ListVRMModels returns built-in and user-imported .vrm model metadata.
func (a *App) ListVRMModels() ([]VRMModelInfo, error) {
	var result []VRMModelInfo
	entries, err := assets.ReadDir("frontend/dist/vrm")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".vrm") {
				info, _ := e.Info()
				sizeKB := 0
				if info != nil {
					sizeKB = int(info.Size() / 1024)
				}
				result = append(result, VRMModelInfo{
					Name:   e.Name(),
					URL:    "/vrm/" + e.Name(),
					Source: "builtin",
					SizeKB: sizeKB,
				})
			}
		}
	}
	userDir := filepath.Join(a.dataDir, "vrm")
	uentries, err := os.ReadDir(userDir)
	if err == nil {
		for _, e := range uentries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".vrm") {
				info, _ := e.Info()
				sizeKB := 0
				if info != nil {
					sizeKB = int(info.Size() / 1024)
				}
				result = append(result, VRMModelInfo{
					Name:   e.Name(),
					URL:    "/user-vrm/" + e.Name(),
					Source: "user",
					SizeKB: sizeKB,
				})
			}
		}
	}
	return result, nil
}

// GetVRMPath returns the asset URL for a given VRM model name.
func (a *App) GetVRMPath(name string) (string, error) {
	if _, err := assets.Open("frontend/dist/vrm/" + name); err == nil {
		return "/vrm/" + name, nil
	}
	userPath := filepath.Join(a.dataDir, "vrm", name)
	if _, err := os.Stat(userPath); err == nil {
		return "/user-vrm/" + name, nil
	}
	return "", fmt.Errorf("VRM model not found: %s", name)
}

// ImportVRMFile decodes a base64-encoded .vrm file and writes it to
// ~/.aiko/vrm/{name}. Validates the glTF magic header before writing.
func (a *App) ImportVRMFile(name string, base64Data string) error {
	if !strings.HasSuffix(name, ".vrm") {
		return fmt.Errorf("filename must end in .vrm")
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("base64 decode: %w", err)
	}
	if len(data) < 4 || string(data[:4]) != "glTF" {
		return fmt.Errorf("not a valid glTF/VRM file")
	}
	userDir := filepath.Join(a.dataDir, "vrm")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return fmt.Errorf("create vrm dir: %w", err)
	}
	dest := filepath.Join(userDir, filepath.Base(name))
	return os.WriteFile(dest, data, 0o644)
}

// DeleteVRMModel removes a user-imported VRM from ~/.aiko/vrm/.
func (a *App) DeleteVRMModel(name string) error {
	userPath := filepath.Join(a.dataDir, "vrm", filepath.Base(name))
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		return fmt.Errorf("user-imported model not found: %s", name)
	}
	return os.Remove(userPath)
}
