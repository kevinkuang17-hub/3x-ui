#!/usr/bin/env bash
# One-click node join: run on a fresh server with the command shown in the master panel
# (Nodes → One-click add). It installs the panel if needed and registers it with the master.
#   bash <(curl -Ls https://raw.githubusercontent.com/kevinkuang17-hub/3x-ui/main/join-node.sh) '<join URL>'
# IP reporter only (Nodes → Domain shows this command for a node linked to a domain):
#   bash <(curl -Ls .../join-node.sh) --report '<report URL>'
# Options (environment): JOIN_ALLOW_HTTP=1 accept a panel without a certificate,
#   JOIN_INSECURE=1 skip checking the master's certificate.
set -euo pipefail

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

die() {
    echo -e "${red}$*${plain}" >&2
    exit 1
}
info() { echo -e "${green}$*${plain}"; }
warn() { echo -e "${yellow}$*${plain}"; }

[[ $EUID -eq 0 || -n "${JOIN_TEST:-}" ]] || die "Please run as root (sudo -i first)."
command -v curl > /dev/null || die "curl is required."

# -4: the master checks that requests come from the IP this server reports.
CURL=(curl -4 -sS --connect-timeout 10 --max-time 90)
[[ "${JOIN_INSECURE:-0}" == "1" ]] && CURL+=(-k)
XUI_BIN="${XUI_BIN:-/usr/local/x-ui/x-ui}"
RESULT_FILE="${RESULT_FILE:-/etc/x-ui/install-result.env}"
STARTED_AT=$(date +%s)
XUI_DIR="${XUI_DIR:-/usr/local/x-ui}"
CRON_FILE="${CRON_FILE:-/etc/cron.d/x-ui-report-ip}"
REPO_RAW="${REPO_RAW:-https://raw.githubusercontent.com/kevinkuang17-hub/3x-ui/main}"

public_ip() {
    local ip svc
    for svc in https://api.ipify.org https://ipv4.icanhazip.com https://ifconfig.me/ip; do
        ip=$(curl -4 -fsS --max-time 8 "$svc" 2> /dev/null | tr -d '[:space:]') || true
        if [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
            echo "$ip"
            return 0
        fi
    done
    return 1
}

json_field() {
    # json_field <name>: read a flat string field from the JSON on stdin without needing jq.
    { grep -o "\"$1\" *: *\"[^\"]*\"" || true; } | head -n1 | sed -E 's/.*: *"([^"]*)"/\1/'
}

# post_json <url> <body>: one request; prints the body, fails with the master's message on non-2xx.
post_json() {
    local url="$1" body="$2" out code msg
    out=$("${CURL[@]}" -w $'\n%{http_code}' -H 'Content-Type: application/json' -d "$body" "$url" 2>&1) ||
        die "Could not reach the master: $out"
    code="${out##*$'\n'}"
    out="${out%$'\n'*}"
    if [[ ! "$code" =~ ^2 ]]; then
        msg=$(echo "$out" | json_field msg)
        die "Master refused the request (HTTP $code): ${msg:-$out}"
    fi
    echo "$out"
}

install_reporter() {
    local url="$1" insecure=""
    [[ "$url" =~ ^https?://[^/]+/.*node-ip/[0-9a-f]{32}$ ]] || die "That does not look like a report URL: $url"
    [[ "${JOIN_INSECURE:-0}" == "1" ]] && insecure="-k"
    mkdir -p "$XUI_DIR"
    cat > "$XUI_DIR/report-ip.sh" << EOF
#!/usr/bin/env bash
# Tells the master this server's current public IP so its domain follows an IP change.
ip=\$(curl -4 -fsS --max-time 10 https://api.ipify.org 2> /dev/null || curl -4 -fsS --max-time 10 https://ipv4.icanhazip.com 2> /dev/null || true)
ip=\$(echo "\$ip" | tr -d '[:space:]')
[[ "\$ip" =~ ^([0-9]{1,3}\\.){3}[0-9]{1,3}\$ ]] || exit 0
curl -4 -fsS $insecure --max-time 30 -H 'Content-Type: application/json' -d "{\\"ip\\":\\"\$ip\\"}" '$url' > /dev/null 2>&1 || true
EOF
    chmod 700 "$XUI_DIR/report-ip.sh"
    echo "*/5 * * * * root $XUI_DIR/report-ip.sh" > "$CRON_FILE"
    chmod 644 "$CRON_FILE"
    info "IP reporter installed: this server tells the master its IP every 5 minutes."
}

if [[ "${1:-}" == "--report" ]]; then
    install_reporter "${2:-}"
    "$XUI_DIR/report-ip.sh" || true
    exit 0
fi

JOIN_URL="${1:-}"
[[ -n "$JOIN_URL" ]] || die "Usage: join-node.sh '<join URL from the master panel>'"
[[ "$JOIN_URL" =~ ^https?://[^/]+/.*node-join/[0-9a-f]{48}$ ]] || die "That does not look like a join URL: $JOIN_URL"
ORIGIN=$(echo "$JOIN_URL" | sed -E 's#^(https?://[^/]+).*#\1#')

info "[1/4] Detecting this server's public IP and region..."
IP="${JOIN_IP:-}"
[[ -n "$IP" ]] || IP=$(public_ip) || die "Could not detect a public IPv4 address."
COUNTRY="${JOIN_COUNTRY:-}"
[[ -n "$COUNTRY" ]] || COUNTRY=$(curl -fsS --max-time 8 "https://ipinfo.io/${IP}/country" 2> /dev/null | tr -d '[:space:]' || true)
[[ "$COUNTRY" =~ ^[A-Za-z]{2}$ ]] || COUNTRY=""
echo "    IP: $IP  region: ${COUNTRY:-unknown}"

info "[2/4] Reserving the node name with the master..."
PREP=$(post_json "${JOIN_URL}/prepare" "{\"ip\":\"${IP}\",\"country\":\"${COUNTRY}\"}")
NAME=$(echo "$PREP" | json_field name)
DOMAIN=$(echo "$PREP" | json_field domain)
[[ -n "$NAME" ]] || die "Unexpected answer from master: $PREP"
echo "    name: $NAME  domain: ${DOMAIN:-(none, using IP)}"

FRESH_INSTALL=0
if [[ -x "$XUI_BIN" ]]; then
    info "[3/4] 3X-UI is already installed; keeping its settings (username, password and database unchanged)."
else
    # The installer runs unattended below, so ask its one real choice here, like a manual install does.
    if [[ -z "${XUI_DB_TYPE:-}" && -t 0 ]]; then
        echo "Database for the new panel:"
        echo "  1) SQLite      (default, recommended below ~500 clients)"
        echo "  2) PostgreSQL  (installed on this server; for many clients)"
        read -rp "Choose [1]: " db_choice || db_choice=""
        if [[ "${db_choice:-1}" == "2" ]]; then
            export XUI_DB_TYPE=postgres
        fi
    fi
    FRESH_INSTALL=1
    info "[3/4] Installing 3X-UI (takes a few minutes)..."
    if [[ -n "$DOMAIN" ]]; then
        export XUI_SSL_MODE=domain XUI_DOMAIN="$DOMAIN"
        # A brand-new record may take a moment to answer; Let's Encrypt asks the authoritative servers.
        sleep "${JOIN_DNS_WAIT:-20}"
    else
        export XUI_SSL_MODE=ip
    fi
    export XUI_NONINTERACTIVE=1
    bash <(curl -Ls "${REPO_RAW}/install.sh") < /dev/null || die "Installation failed; see the output above."
fi

SHOW=$("$XUI_BIN" setting -show 2> /dev/null || true)
PORT=$(echo "$SHOW" | sed -n 's/^port: *//p' | head -n1 | tr -d '[:space:]')
BASE=$(echo "$SHOW" | sed -n 's/^webBasePath: *//p' | head -n1 | tr -d '[:space:]')
CERT=$("$XUI_BIN" setting -getCert 2> /dev/null | sed -n 's/^cert: *//p' | head -n1 | tr -d '[:space:]')
[[ "$PORT" =~ ^[0-9]+$ ]] || die "Could not read the panel port (x-ui setting -show)."
SCHEME=https
ALLOW_HTTP=false
if [[ -z "$CERT" ]]; then
    if [[ "${JOIN_ALLOW_HTTP:-0}" != "1" ]]; then
        die "The panel has no SSL certificate (Let's Encrypt needs port 80 open to this server).
Open port 80 in the firewall/security group, add a certificate with the 'x-ui' menu (SSL options), then re-run this command.
To join over plain HTTP anyway (not recommended), re-run with JOIN_ALLOW_HTTP=1 in front."
    fi
    SCHEME=http
    ALLOW_HTTP=true
fi
TOKEN=$("$XUI_BIN" setting -getApiToken -tokenName master-join -tokenScope admin 2> /dev/null | sed -n 's/^apiToken: *//p' | head -n1 | tr -d '[:space:]')
[[ -n "$TOKEN" ]] || die "Could not create an API token (x-ui setting -getApiToken)."

if command -v ufw > /dev/null && ufw status 2> /dev/null | grep -q "Status: active"; then
    ufw allow "${PORT}/tcp" > /dev/null || warn "Could not open port ${PORT} in ufw."
fi

# The installer restarts the panel; wait until it listens before asking the master to probe it.
for _ in $(seq 1 30); do
    (exec 3<> "/dev/tcp/127.0.0.1/${PORT}") 2> /dev/null && break
    sleep 2
done

info "[4/4] Registering with the master (it checks it can reach this panel)..."
FIN=$(post_json "${JOIN_URL}/finish" "{\"scheme\":\"${SCHEME}\",\"port\":${PORT},\"basePath\":\"${BASE}\",\"apiToken\":\"${TOKEN}\",\"allowHttp\":${ALLOW_HTTP}}")
ADDR=$(echo "$FIN" | json_field address)
REPORT=$(echo "$FIN" | json_field reportPath)
if [[ -n "$REPORT" ]]; then
    install_reporter "${ORIGIN}${REPORT}"
fi
echo
info "Done! This server is now node \"${NAME}\" (${ADDR:-$IP}) in the master panel."
warn "Make sure port ${PORT}/tcp is open in your cloud provider's firewall/security group."

# Repeat the login details the installer printed, which scrolled away above.
if [[ "$FRESH_INSTALL" == "1" && -f "$RESULT_FILE" ]] && [[ $(stat -c %Y "$RESULT_FILE" 2> /dev/null || echo 0) -ge $STARTED_AT ]]; then
    (
        # shellcheck disable=SC1090
        . "$RESULT_FILE"
        echo
        info "═══════════════ Panel login for this node ═══════════════"
        echo "  Username:    ${XUI_USERNAME:-}"
        echo "  Password:    ${XUI_PASSWORD:-}"
        echo "  Port:        ${XUI_PANEL_PORT:-$PORT}"
        echo "  WebBasePath: ${XUI_WEB_BASE_PATH:-$BASE}"
        echo "  Access URL:  ${XUI_ACCESS_URL:-}"
        echo "  API Token:   ${XUI_API_TOKEN:-}"
        echo "  Database:    ${XUI_DB_TYPE:-sqlite}"
        info "═════════════════════════════════════════════════════════"
        warn "Save these now. They are also kept in ${RESULT_FILE} (root only)."
    )
fi
