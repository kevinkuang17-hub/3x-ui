// The panel runs outside China, so whether China can reach a server is checked from ITDOG's
// probes there (about 200, every province and carrier); the panel only builds the links.
const ITDOG = 'https://www.itdog.cn';

// Protocols that only listen on UDP; a TCP probe of their port says nothing.
const UDP_ONLY = new Set(['hysteria', 'tuic', 'wireguard', 'amneziawg']);

export interface ChinaCheckLinks {
  ping: string;
  tcping: string;
}

function bareHost(address: string): string {
  return address.trim().replace(/^\[|\]$/g, '');
}

export function chinaCheckLinks(
  address: string,
  port: number,
  protocol: string,
): ChinaCheckLinks | null {
  const host = bareHost(address);
  if (!host) return null;
  const v6 = host.includes(':');
  const ping = `${ITDOG}/${v6 ? 'ping_ipv6' : 'ping'}/${host}`;
  const tcping =
    UDP_ONLY.has(protocol) || port <= 0
      ? ''
      : `${ITDOG}/${v6 ? 'tcping_ipv6' : 'tcping'}/${v6 ? `[${host}]` : host}:${port}`;
  return { ping, tcping };
}
