package shellenv

const zshRCOMZ = `# narc session zshrc (oh-my-zsh variant)
# Source the user's real zshrc — this loads oh-my-zsh and all its hooks.
ZDOTDIR="${NARC_REAL_ZDOTDIR}"
[[ -f "${NARC_REAL_ZDOTDIR}/.zshrc" ]] && source "${NARC_REAL_ZDOTDIR}/.zshrc"

# Register a persistent precmd hook that prepends (narc) to PROMPT.
# The guard prevents double-prefixing on repeated redraws.
# precmd_functions is an array; += appends safely even if it didn't exist before.
_narc_precmd() {
    [[ "$PROMPT" != "(narc)"* ]] && PROMPT="(narc) $PROMPT"
}
precmd_functions+=(_narc_precmd)
`
