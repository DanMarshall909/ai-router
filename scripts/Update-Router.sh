#!/usr/bin/env bash
set -euo pipefail

debug_log_enabled=false
for argument in "$@"; do
  case "$argument" in
    --debug-log)
      debug_log_enabled=true
      ;;
    --help)
      printf '%s\n' "Usage: $0 [--debug-log]"
      exit 0
      ;;
    *)
      printf '%s\n' "Unknown option: $argument" >&2
      exit 1
      ;;
  esac
done

readonly SCRIPT_DIRECTORY="$(dirname "$(readlink -f "$0")")"
readonly REPOSITORY_ROOT="$(dirname "$SCRIPT_DIRECTORY")"
readonly BINARY_PATH="$REPOSITORY_ROOT/ai-router"
readonly TEMPORARY_BINARY_PATH="$(mktemp "$REPOSITORY_ROOT/.ai-router.XXXXXX")"
readonly BUILD_TIME="$(date --utc +%Y-%m-%dT%H:%M:%SZ)"
readonly SERVICE_NAME="ai-router"
readonly CONFIGURATION_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
readonly CONFIGURATION_PATH="${AI_ROUTER_CONFIG:-$CONFIGURATION_HOME/ai-router/config.json}"
readonly SYSTEMD_USER_DIRECTORY="$CONFIGURATION_HOME/systemd/user"
readonly SERVICE_PATH="$SYSTEMD_USER_DIRECTORY/$SERVICE_NAME.service"

debug_log_argument=""
if [ "$debug_log_enabled" = true ]; then
  debug_log_argument=" -debug-log"
fi

cleanup() {
  rm -f "$TEMPORARY_BINARY_PATH"
}
trap cleanup EXIT

if [ ! -f "$CONFIGURATION_PATH" ]; then
  printf '%s\n' "Router configuration was not found at $CONFIGURATION_PATH. Set AI_ROUTER_CONFIG to use a different path." >&2
  exit 1
fi

mkdir -p "$SYSTEMD_USER_DIRECTORY"
printf '%s\n' \
  '[Unit]' \
  'Description=AI Router' \
  'After=network-online.target' \
  'Wants=network-online.target' \
  '' \
  '[Service]' \
  'Type=simple' \
  "WorkingDirectory=$REPOSITORY_ROOT" \
  "ExecStart=\"$BINARY_PATH\" -config \"$CONFIGURATION_PATH\"$debug_log_argument" \
  'Restart=always' \
  'RestartSec=5' \
  '' \
  '[Install]' \
  'WantedBy=default.target' \
  > "$SERVICE_PATH"

systemctl --user daemon-reload
systemctl --user enable "$SERVICE_NAME"

go -C "$REPOSITORY_ROOT" build -ldflags "-X main.buildTime=$BUILD_TIME" -o "$TEMPORARY_BINARY_PATH" ./cmd/airouter
mv "$TEMPORARY_BINARY_PATH" "$BINARY_PATH"
systemctl --user restart "$SERVICE_NAME"
systemctl --user --no-pager --full status "$SERVICE_NAME"
