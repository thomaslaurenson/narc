package shellenv

func buildFishEnv(baseEnv []string) ([]string, func(), error) {
	env := setEnvVar(baseEnv, "SHELL_PROMPT_PREFIX", "(narc) ")
	return env, func() {}, nil
}
