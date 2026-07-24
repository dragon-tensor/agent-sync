package providers

type Config struct {
	DataDir            string
	ClaudeCodePath     string
	OpenCodePath       string
	CursorPath         string
	ChatGPTExportPath  string
	ClaudeWebExportPath string
	GeminiExportPath    string
	CopilotPath         string
	CodexPath           string
	GenericImportPath   string
	AutoDetectPaths    bool
}
