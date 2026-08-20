# ignore config for non-interactive shells
if not $nu.is-interactive {
  return
}

# general settings
$env.config.show_banner = false
$env.config.edit_mode = "vi"
$env.config.buffer_editor = "v"

# aliases
alias ..      = cd ..
alias ...     = cd ...
alias ....    = cd ....
alias .....   = cd .....
alias ......  = cd ......

alias c       = claude
alias cl      = clear
alias d       = colima start
alias ds      = colima stop
alias dlz     = d | lzd
alias de      = direnv
alias dv      = devenv
alias l       = eza -la --icons --git --sort type
alias lt      = l --tree --level=2 --long
alias la      = ^tree
alias lz      = lazygit
alias lzd     = lazydocker
alias lzq     = lazysql
alias rd      = rm -rf
alias spt     = spotify_player
alias tp      = btop
alias ts      = tailscale
alias v       = nvim
alias x       = exit
alias y       = yazi
alias za      = zellij a
alias zz      = zellij

alias nu-open = open
alias open    = ^open

# navigation functions
def _fselect [...args] {
  let last = (if ($args | is-empty) { "" } else { $args | last })
  let q = if ($last == "") { [] } else { ["--query" $last] }

  ^fd ...$args
  | ^fzf --height "40%" --reverse --preview 'tree -C {} | head -100' ...$q
}

def cx [dir?: string] {
  if $dir != null { cd $dir }
  l
}

def md [dir: string] {
  mkdir $dir
  cd $dir
}

def ff [] {
  ^aerospace list-windows --all
  | ^fzf --bind 'enter:execute(bash -c "aerospace focus --window-id {1}")+abort'
}

def klp [...ports: int] {
  for port in $ports {
    let result = (do { ^lsof -ti $":($port)" } | complete)
    if ($result.stdout | str trim | is-empty) {
      print $"no process on port ($port)"
    } else {
      $result.stdout | lines | where { ($in | str trim) != "" } | each { |pid|
        ^kill -9 ($pid | str trim)
      }
      print $"killed port ($port)"
    }
  }
}

# tools
let autoload_dir = ($nu.data-dir | path join "vendor/autoload")
mkdir $autoload_dir

# atuin - shell history
atuin init nu | save -f ($autoload_dir | path join "atuin.nu")

# carapace - completions
$env.CARAPACE_BRIDGES = 'zsh,fish,bash,inshellisense'
carapace _carapace nushell | save -f ($autoload_dir | path join "carapace.nu")

# starship - prompt
starship init nu | save -f ($autoload_dir | path join "starship.nu")

# zoxide - directory jumping
zoxide init nushell | save -f ($autoload_dir | path join "zoxide.nu")

# direnv - directory-specific environments
# Adapted from the MIT-licensed Nushell nu_scripts Direnv hook.
$env.config.hooks.env_change.PWD = (
  $env.config.hooks.env_change.PWD | append { ||
    if (which direnv | is-empty) {
      return
    }

    let userland_direnv = (direnv export json | complete)
    if $userland_direnv.exit_code != 0 or ($userland_direnv.stdout | str trim | is-empty) {
      return
    }

    $userland_direnv.stdout | from json | load-env
    $env.PATH = $env.PATH | split row (char env_sep)
  }
)

# 1password - password manager
$env.OP_PLUGIN_ALIASES_SOURCED = '1'
alias gh = op plugin run -- gh

# $env.config.keybindings ++= [{
#     name: complete_hint
#     modifier: control
#     keycode: char_f
#     mode: [emacs, vi_insert, vi_normal]
#     event: { send: historyhintcomplete }
# }, {
#   name: atuin_in_vi_normal
#   modifier: none
#   keycode: char_k
#   mode: [vi_normal]
#   event: {
#     send: executehostcommand
#     cmd: "with-env { ATUIN_LOG: error, ATUIN_QUERY: (commandline) } { commandline edit (run-external atuin search "--interactive"  e>| str trim) }"
#   }
# }]
