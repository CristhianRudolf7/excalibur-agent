package daemon

func principalUserEnv() []string       { return []string{"USER", "LOGNAME"} }
func defaultPrincipalLanguage() string { return "en" }
