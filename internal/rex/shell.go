package rex

import (
	"os"
	"path/filepath"
	"strings"
)

// zshEnv injects prompt markers without changing the user's startup files.
// zsh restores ZDOTDIR before reading the rest of the user's configuration.
func zshEnv(path string, env []string) ([]string, string, error) {
	if filepath.Base(path) != "zsh" {
		return env, "", nil
	}
	dir, err := os.MkdirTemp(Dir(), "zsh-")
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte(zshIntegration), 0o600); err != nil {
		os.RemoveAll(dir)
		return nil, "", err
	}
	var zdot string
	var hadZdot bool
	var clean []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "ZDOTDIR="); ok {
			zdot, hadZdot = v, true
		} else {
			clean = append(clean, kv)
		}
	}
	if hadZdot {
		clean = append(clean, "RETTY_ZDOTDIR="+zdot)
	}
	return append(clean, "ZDOTDIR="+dir), dir, nil
}

// OSC 133 identifies the active prompt so libghostty can clear it on
// resize and let zsh redraw it, instead of wrapping its old right prompt.
const zshIntegration = `if (( ${+RETTY_ZDOTDIR} )); then
  export ZDOTDIR=$RETTY_ZDOTDIR
  unset RETTY_ZDOTDIR
else
  unset ZDOTDIR
fi
[[ ! -r ${ZDOTDIR-$HOME}/.zshenv ]] || source -- "${ZDOTDIR-$HOME}/.zshenv"
[[ -o interactive ]] || return

# Powerlevel10k rebuilds PS1 while expanding it. Let it emit its own
# markers so asynchronous theme updates keep the prompt marked too.
typeset -gi __p9k_force_term_shell_integration=1

_retty_mark_prompt() {
  local code=$?
  emulate -L zsh
  if ! zle; then
    printf '\e]133;D;%d\a' $code
  fi
  [[ -o promptpercent ]] || return
  local start=$'%{\e]133;A;cl=line;redraw=1\a%}' input=$'%{\e]133;B\a%}'
  local continuation=$'%{\e]133;P;k=s\a%}'
  if [[ $PS1 != *$'\e]133;'* ]]; then
    PS1=$start${PS1//$'\n'/$'\n'$continuation}$input
  fi
  [[ $PS2 == *$'\e]133;'* ]] || PS2=$continuation$PS2$input
}
_retty_command_start() {
  printf '\e]133;C\a'
}
_retty_install_marks() {
  emulate -L zsh
  # Login startup files can rebuild PATH. Keep the pane's Codex launcher first.
  [[ -z $RETTY_CODEX_BIN ]] || path=($RETTY_CODEX_BIN ${path:#$RETTY_CODEX_BIN})
  autoload -Uz add-zsh-hook
  add-zsh-hook -d precmd _retty_install_marks
  add-zsh-hook precmd _retty_mark_prompt
  add-zsh-hook preexec _retty_command_start
  _retty_mark_prompt
}
typeset -ga precmd_functions
precmd_functions+=(_retty_install_marks)
`
