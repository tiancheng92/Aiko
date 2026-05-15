//go:build !darwin

package main

func openSettingsPanel(_ string) {}
func closeSettingsPanel()        {}
func isSettingsPanelVisible() bool { return false }
