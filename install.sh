#!/bin/sh
# Installs the latest (or a pinned) godev release binary for Linux/macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/abtinokhovat/godev/main/install.sh | sh
#
# Env vars:
#   VERSION      release tag to install, e.g. v0.2.0 (default: latest)
#   INSTALL_DIR  where to put the binary (default: /usr/local/bin if
#                writable, else $HOME/.local/bin)
set -eu

repo="abtinokhovat/godev"

os=""
case "$(uname -s)" in
	Linux) os="linux" ;;
	Darwin) os="darwin" ;;
	*)
		echo "error: no prebuilt godev binary for '$(uname -s)'." >&2
		echo "Windows: download godev-windows-amd64.zip from https://github.com/$repo/releases" >&2
		exit 1
		;;
esac

arch=""
case "$(uname -m)" in
	x86_64 | amd64) arch="amd64" ;;
	arm64 | aarch64) arch="arm64" ;;
	*)
		echo "error: no prebuilt godev binary for architecture '$(uname -m)'." >&2
		exit 1
		;;
esac

asset="godev-${os}-${arch}.tar.gz"
if [ -n "${VERSION:-}" ]; then
	url="https://github.com/${repo}/releases/download/${VERSION}/${asset}"
else
	url="https://github.com/${repo}/releases/latest/download/${asset}"
fi

fetch() {
	# $1 = url, $2 = output path
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -q "$1" -O "$2"
	else
		echo "error: need curl or wget to download godev." >&2
		exit 1
	fi
}

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading $url"
if ! fetch "$url" "$tmpdir/$asset"; then
	echo "error: download failed - is '${VERSION:-latest}' a real release? https://github.com/${repo}/releases" >&2
	exit 1
fi

tar -xzf "$tmpdir/$asset" -C "$tmpdir"
chmod +x "$tmpdir/godev"

if [ -n "${INSTALL_DIR:-}" ]; then
	install_dir="$INSTALL_DIR"
elif [ -w /usr/local/bin ] 2>/dev/null; then
	install_dir=/usr/local/bin
else
	install_dir="$HOME/.local/bin"
fi
mkdir -p "$install_dir"
mv "$tmpdir/godev" "$install_dir/godev"
echo "Installed godev to $install_dir/godev"

case ":${PATH}:" in
	*":${install_dir}:"*)
		echo "godev is ready - run 'godev help' to get started."
		exit 0
		;;
esac

# install_dir isn't on PATH yet - add it to the current shell's rc file
# (idempotent: skip if already present, e.g. from a previous run).
rc=""
case "$(basename "${SHELL:-}")" in
	zsh) rc="$HOME/.zshrc" ;;
	bash) rc="$HOME/.bashrc" ;;
	fish) rc="$HOME/.config/fish/config.fish" ;;
esac

path_line="export PATH=\"$install_dir:\$PATH\""
if [ "$(basename "${SHELL:-}")" = "fish" ]; then
	path_line="set -gx PATH $install_dir \$PATH"
fi

if [ -n "$rc" ]; then
	mkdir -p "$(dirname "$rc")"
	if [ ! -f "$rc" ] || ! grep -qF "$install_dir" "$rc" 2>/dev/null; then
		printf '\n# added by godev'"'"'s install.sh\n%s\n' "$path_line" >>"$rc"
		echo "Added $install_dir to PATH in $rc"
		echo "Run 'source $rc' (or open a new terminal), then 'godev help' to get started."
	else
		echo "godev is ready - open a new terminal (PATH already configured in $rc), then run 'godev help'."
	fi
else
	echo "Add $install_dir to your PATH to finish (unrecognized shell '$SHELL' - edit its rc file manually):"
	echo "  $path_line"
fi
