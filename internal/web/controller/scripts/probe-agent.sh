#!/bin/sh
# ISP probe agent for 3X-UI: tests from this network whether node lines can be reached and
# reports to the master. POSIX sh, so it runs on OpenWrt routers (busybox) as well as Linux.
#   install:  curl -fsSL '<probe URL>/agent.sh' | sh -s -- install '<probe URL>'   (served by the master)
#   run once: sh /etc/x-ui-probe/probe-agent.sh run
#   remove:   sh /etc/x-ui-probe/probe-agent.sh uninstall
set -eu

DIR="${PROBE_DIR:-/etc/x-ui-probe}"
CONF="$DIR/url"
SELF="$DIR/probe-agent.sh"
TIMEOUT="${PROBE_TIMEOUT:-3}"

die() {
    echo "probe-agent: $*" >&2
    exit 1
}

cron_file() {
    if [ -d /etc/crontabs ]; then
        echo /etc/crontabs/root
    else
        echo /etc/cron.d/x-ui-probe
    fi
}

install_agent() {
    url="$1"
    echo "$url" | grep -Eq '^https?://[^/]+/.*probe/[0-9a-f]{40}$' || die "that does not look like a probe URL: $url"
    command -v curl > /dev/null 2>&1 || die "curl is required (opkg install curl)"
    mkdir -p "$DIR"
    printf '%s\n' "$url" > "$CONF"
    chmod 600 "$CONF"
    curl -fsSL "$url/agent.sh" -o "$SELF.tmp" || die "could not download the agent from the master"
    mv "$SELF.tmp" "$SELF"
    chmod 700 "$SELF"
    cf=$(cron_file)
    line="*/5 * * * * sh $SELF run > /dev/null 2>&1"
    if [ "$cf" = /etc/crontabs/root ]; then
        touch "$cf"
        grep -v "x-ui-probe" "$cf" > "$cf.tmp" || true
        echo "$line" >> "$cf.tmp"
        mv "$cf.tmp" "$cf"
        /etc/init.d/cron restart > /dev/null 2>&1 || true
    else
        echo "*/5 * * * * root sh $SELF run > /dev/null 2>&1" > "$cf"
        chmod 644 "$cf"
    fi
    echo "probe-agent: installed; checking lines every 5 minutes."
    sh "$SELF" run && echo "probe-agent: first report sent."
}

uninstall_agent() {
    cf=$(cron_file)
    if [ "$cf" = /etc/crontabs/root ]; then
        grep -v "x-ui-probe" "$cf" > "$cf.tmp" 2> /dev/null || true
        mv "$cf.tmp" "$cf" 2> /dev/null || true
        /etc/init.d/cron restart > /dev/null 2>&1 || true
    else
        rm -f "$cf"
    fi
    rm -rf "$DIR"
    echo "probe-agent: removed."
}

# tcp_tool picks how to test a TCP connect: curl's telnet:// (with timing) or nc -z.
tcp_tool() {
    if curl -V 2> /dev/null | grep -q telnet; then
        echo curl
    elif nc -h 2>&1 | grep -q -- '-z'; then
        echo nc
    else
        echo none
    fi
}

# check <host> <port>: prints the TCP connect time in ms (0 when the tool cannot time it),
# or -1 when unreachable.
check() {
    if [ "$TOOL" = curl ]; then
        t=$(curl -s -o /dev/null -w '%{time_connect}' --connect-timeout "$TIMEOUT" --max-time "$((TIMEOUT + 1))" "telnet://$1:$2" < /dev/null 2> /dev/null || true)
        awk -v t="${t:-0}" 'BEGIN { if (t + 0 > 0) printf "%d", t * 1000 + 0.5; else print -1 }'
    elif nc -z -w "$TIMEOUT" "$1" "$2" < /dev/null > /dev/null 2>&1; then
        echo 0
    else
        echo -1
    fi
}

run_agent() {
    [ -f "$CONF" ] || die "not installed"
    TOOL=$(tcp_tool)
    # Without a TCP tool every line would look blocked; report nothing rather than lie.
    [ "$TOOL" != none ] || die "neither curl with telnet support nor nc -z is available (opkg install curl)"
    # One run at a time: a slow round must not overlap the next cron tick.
    mkdir "$DIR/lock" 2> /dev/null || exit 0
    trap 'rmdir "$DIR/lock" 2> /dev/null' EXIT INT TERM
    url=$(cat "$CONF")
    targets=$(curl -fsS --max-time 30 "$url") || die "could not fetch targets"
    body='{"results":['
    sep=""
    # Read from a here-doc so the loop runs in this shell, not a pipeline subshell.
    while read -r id host port; do
        [ -n "$id" ] || continue
        ms=$(check "$host" "$port")
        if [ "$ms" -ge 0 ] 2> /dev/null; then
            body="$body$sep{\"inboundId\":$id,\"ok\":true,\"latencyMs\":$ms}"
        else
            body="$body$sep{\"inboundId\":$id,\"ok\":false,\"latencyMs\":0}"
        fi
        sep=","
    done << EOF
$targets
EOF
    body="$body]}"
    curl -fsS --max-time 30 -H 'Content-Type: application/json' -d "$body" "$url" > /dev/null || die "could not send the report"
}

case "${1:-}" in
    install) install_agent "${2:-}" ;;
    run) run_agent ;;
    uninstall) uninstall_agent ;;
    *) die "usage: probe-agent.sh install '<probe URL>' | run | uninstall" ;;
esac
