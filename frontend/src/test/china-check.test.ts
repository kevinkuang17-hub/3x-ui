import { describe, expect, it } from 'vitest';

import { chinaCheckLinks } from '@/lib/china-check';

describe('chinaCheckLinks', () => {
  it('pings the host and TCP-pings the inbound port', () => {
    expect(chinaCheckLinks('hk.example.com', 443, 'vless')).toEqual({
      ping: 'https://www.itdog.cn/ping/hk.example.com',
      tcping: 'https://www.itdog.cn/tcping/hk.example.com:443',
    });
  });

  it('skips the TCP probe for a UDP-only protocol such as Hysteria2', () => {
    expect(chinaCheckLinks('203.0.113.5', 443, 'hysteria')?.tcping).toBe('');
  });

  it('uses the IPv6 tools for an IPv6 address', () => {
    expect(chinaCheckLinks('[2001:db8::1]', 443, 'vless')).toEqual({
      ping: 'https://www.itdog.cn/ping_ipv6/2001:db8::1',
      tcping: 'https://www.itdog.cn/tcping_ipv6/[2001:db8::1]:443',
    });
  });

  it('has nothing to check without an address', () => {
    expect(chinaCheckLinks('', 443, 'vless')).toBeNull();
  });
});
