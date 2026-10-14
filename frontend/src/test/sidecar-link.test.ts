import { describe, expect, it } from 'vitest';

import { genLink } from '@/lib/xray/inbound-link';
import { InboundSchema } from '@/schemas/api/inbound';

function sidecar(protocol: 'anytls' | 'naive', serverName: string) {
  return InboundSchema.parse({
    id: 7,
    port: 443,
    protocol,
    remark: 'HK A',
    tag: `in-${protocol}`,
    settings: { clients: [{ email: 'ivy@hk', password: 'p/w', enable: true }] },
    streamSettings: {
      network: 'tcp',
      security: 'tls',
      tcpSettings: {},
      tlsSettings: { serverName, certificates: [] },
    },
  });
}

const client = { email: 'ivy@hk', password: 'p/w' };

// The Go subscription (internal/sub/sidecar_links.go) emits the same strings.
describe('sidecar share links', () => {
  it('AnyTLS names the certificate domain as sni while dialing the IP', () => {
    const link = genLink({
      inbound: sidecar('anytls', 'hk.example.com'),
      address: '203.0.113.5',
      remark: 'HK A',
      client,
    });
    expect(link).toBe('anytls://p%2Fw@203.0.113.5:443/?sni=hk.example.com#HK%20A');
  });

  it('AnyTLS on a bare IP without a server name sends no sni', () => {
    const link = genLink({ inbound: sidecar('anytls', ''), address: '203.0.113.5', client });
    expect(link).toBe('anytls://p%2Fw@203.0.113.5:443/');
  });

  it('NaiveProxy dials the certificate domain with the email as user', () => {
    const link = genLink({
      inbound: sidecar('naive', 'hk.example.com'),
      address: '203.0.113.5',
      remark: 'HK A',
      client,
    });
    expect(link).toBe('naive+https://ivy%40hk:p%2Fw@hk.example.com:443#HK%20A');
  });
});
