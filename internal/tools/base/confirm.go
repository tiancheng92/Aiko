package base

// ShellConfirmInfo is sent to the frontend when execute_shell requests user confirmation.
type ShellConfirmInfo struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir"`
}

// ConfirmResult is the user's response to a tool confirmation request.
type ConfirmResult struct {
	Approved      bool   `json:"approved"`
	EditedContent string `json:"edited_content"` // user-edited command or code
}

// CodeConfirmInfo is sent to the frontend when execute_code requests user confirmation.
type CodeConfirmInfo struct {
	ID         string `json:"id"`
	Language   string `json:"language"`
	Code       string `json:"code"`
	WorkingDir string `json:"working_dir"`
}

// PersistBeforeRestartKey is a context key. agent.go stores a func(string) under
// this key before calling drainRunnerMsg; the update tool retrieves and calls it
// to flush the current conversation turn to SQLite before the binary is replaced.
type PersistBeforeRestartKey struct{}

// UpdateConfirmInfo is sent to the frontend when check_and_update requests confirmation.
type UpdateConfirmInfo struct {
	ID             string `json:"id"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	DownloadURL    string `json:"download_url"`
}
