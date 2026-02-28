package updater

const (
	ErrCodeNetCheckverRequest = "NET_CHECKVER_REQUEST"
	ErrCodeNetCheckverHTTP    = "NET_CHECKVER_HTTP"
	ErrCodeNetDownload        = "NET_DOWNLOAD"

	ErrCodePkgDownload = "PKG_DOWNLOAD"
	ErrCodePkgVerify   = "PKG_VERIFY"
	ErrCodePkgExtract  = "PKG_EXTRACT"

	ErrCodeScriptPreInstall = "SCRIPT_PREINSTALL"

	ErrCodeSwitchPrompt      = "SWITCH_PROMPT"
	ErrCodeSwitchProcess     = "SWITCH_PROCESS"
	ErrCodeSwitchCurrent     = "SWITCH_CURRENT"
	ErrCodeSwitchHealthcheck = "SWITCH_HEALTHCHECK"
	ErrCodeSwitchRollback    = "SWITCH_ROLLBACK"
)
