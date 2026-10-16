#!/bin/sh
# ISP probe agent for 3X-UI: tests from this network whether node lines can be reached and
# reports to the master. POSIX sh, so it runs on OpenWrt routers (busybox) as well as Linux.
#   install:  curl -fsSL '<probe URL>/agent.sh' | sh -s -- install '<probe URL>'   (served by the master)
#   enroll:   curl -fsSL '<enroll URL>/agent.sh' | sh -s -- enroll '<enroll URL>'  (one command, any router)
#   run once: sh /etc/x-ui-probe/probe-agent.sh run
#   remove:   sh /etc/x-ui-probe/probe-agent.sh uninstall
set -eu

DIR="${PROBE_DIR:-/etc/x-ui-probe}"
CONF="$DIR/url"
SELF="$DIR/probe-agent.sh"
TIMEOUT="${PROBE_TIMEOUT:-3}"
# A site every network in China reaches: when it fails, this router is offline and reports nothing.
CONTROL_HOST="${PROBE_CONTROL:-www.baidu.com}"

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

# device_id is a stable, anonymous id for this router, so enrolling it twice reuses its probe.
device_id() {
    for f in /etc/machine-id /sys/class/net/br-lan/address /sys/class/net/eth0/address; do
        if [ -s "$f" ]; then
            md5sum < "$f" | cut -c1-32
            return
        fi
    done
    (cat /proc/sys/kernel/hostname 2> /dev/null || hostname) | md5sum | cut -c1-32
}

# location asks a Chinese IP service where this line is, e.g. "中国 广东 广州 电信"; the panel
# names the probe after it and reads the carrier from it.
location() {
    curl -fsS --max-time 8 https://myip.ipip.net 2> /dev/null | sed -n 's/.*来自于：//p' | tr -s ' ' | sed 's/ *$//' | head -n 1
}

enroll_agent() {
    url="$1"
    echo "$url" | grep -Eq '^https?://[^/]+/.*probe-enroll/[0-9a-f]{32}$' || die "that does not look like an enrollment URL: $url"
    command -v curl > /dev/null 2>&1 || die "curl is required (opkg update && opkg install curl)"
    if [ -f "$CONF" ]; then
        echo "probe-agent: this router is already a probe; nothing to do."
        exit 0
    fi
    host=$(cat /proc/sys/kernel/hostname 2> /dev/null || hostname 2> /dev/null || echo router)
    loc=$(location)
    name="$host"
    [ -z "$loc" ] || name="$loc · $host"
    token=$(curl -fsS --max-time 30 --data-urlencode "deviceId=$(device_id)" --data-urlencode "name=$name" "$url") || die "the panel refused the enrollment (was the key reset?)"
    echo "$token" | grep -Eq '^[0-9a-f]{40}$' || die "unexpected answer from the panel: $token"
    echo "probe-agent: registered as \"$name\"."
    install_agent "${url%%/probe-enroll/*}/probe/$token"
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

# ping_ms <host>: average ICMP round trip in ms, -1 when lost, 0 when ping is unavailable.
ping_ms() {
    command -v ping > /dev/null 2>&1 || {
        echo 0
        return
    }
    out=$(ping -c 3 -W 2 "$1" 2> /dev/null || true)
    avg=$(echo "$out" | sed -n 's#.*= [0-9.]*/\([0-9.]*\)/.*#\1#p' | head -n 1)
    if [ -n "$avg" ]; then
        awk -v t="$avg" 'BEGIN { v = int(t + 0.5); if (v < 1) v = 1; print v }'
    else
        echo -1
    fi
}

# is_private <host>: loopback/LAN addresses answer instantly by nature.
is_private() {
    case "$1" in
        127.* | 10.* | 192.168.* | 172.1[6-9].* | 172.2[0-9].* | 172.3[01].* | localhost) return 0 ;;
    esac
    return 1
}

# self_update fetches the current agent from the master once a day; it takes effect next run.
self_update() {
    stamp="$DIR/updated"
    [ -z "$(find "$stamp" -mtime -1 2> /dev/null)" ] || return 0
    if curl -fsSL --max-time 30 "$1/agent.sh" -o "$SELF.tmp" 2> /dev/null && grep -q "probe-agent" "$SELF.tmp"; then
        mv "$SELF.tmp" "$SELF"
        chmod 700 "$SELF"
    fi
    rm -f "$SELF.tmp"
    touch "$stamp"
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
    self_update "$url"
    [ "$(check "$CONTROL_HOST" 443)" -ge 0 ] 2> /dev/null || die "cannot reach $CONTROL_HOST; this router looks offline, nothing reported"
    targets=$(curl -fsS --max-time 30 "$url") || die "could not fetch targets"
    pings=""
    body='{"results":['
    sep=""
    # Read from a here-doc so the loop runs in this shell, not a pipeline subshell.
    while read -r id host port; do
        [ -n "$id" ] || continue
        ms=$(check "$host" "$port")
        # One ping per host, however many lines share it.
        ping=$(printf '%s\n' "$pings" | awk -v h="$host" '$1 == h { print $2; exit }')
        if [ -z "$ping" ]; then
            ping=$(ping_ms "$host")
            pings="$pings
$host $ping"
        fi
        # OpenClash proxying the router's own traffic answers at once from the router itself:
        # such a result says nothing about the path to the server.
        proxied=false
        if ! is_private "$host" && { [ "$TOOL" = curl ] && [ "$ms" -ge 0 ] && [ "$ms" -le 1 ]; } 2> /dev/null; then
            proxied=true
        fi
        if [ "$ms" -ge 0 ] 2> /dev/null; then
            body="$body$sep{\"inboundId\":$id,\"ok\":true,\"latencyMs\":$ms,\"pingMs\":$ping,\"proxied\":$proxied}"
        else
            body="$body$sep{\"inboundId\":$id,\"ok\":false,\"latencyMs\":0,\"pingMs\":$ping,\"proxied\":false}"
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
    enroll) enroll_agent "${2:-}" ;;
    run) run_agent ;;
    uninstall) uninstall_agent ;;
    *) die "usage: probe-agent.sh install '<probe URL>' | enroll '<enroll URL>' | run | uninstall" ;;
esac
