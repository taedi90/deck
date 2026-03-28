package preparecli

import "strings"

func renderLauncherScript() string {
	return strings.TrimSpace(`#!/bin/sh
set -eu

os_name="$(uname -s)"
arch_name="$(uname -m)"

case "$os_name" in
	Linux) deck_os="linux" ;;
	Darwin) deck_os="darwin" ;;
	*)
		echo "deck: unsupported OS: $os_name" >&2
		exit 1
		;;
esac

case "$arch_name" in
	x86_64|amd64) deck_arch="amd64" ;;
	aarch64|arm64) deck_arch="arm64" ;;
	*)
		echo "deck: unsupported architecture: $arch_name" >&2
		exit 1
		;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
runtime_bin="$script_dir/outputs/bin/$deck_os/$deck_arch/deck"

if [ ! -x "$runtime_bin" ]; then
	if [ -e "$runtime_bin" ]; then
		echo "deck: runtime binary is not executable: outputs/bin/$deck_os/$deck_arch/deck" >&2
	else
		echo "deck: bundle does not include a runtime binary for $deck_os/$deck_arch" >&2
	fi
	exit 1
fi

exec "$runtime_bin" "$@"`)
}
