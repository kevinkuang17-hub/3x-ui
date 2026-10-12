import { describe, expect, it } from 'vitest';

import { InboundFormSchema } from '@/schemas/forms/inbound-form';
import { formValuesToWirePayload } from '@/lib/xray/inbound-form-adapter';
import {
  QUICK_PRESETS,
  assignPorts,
  buildQuickInbound,
  findDuplicate,
  presetById,
  uniqueRemark,
  type QuickInboundInput,
  type QuickPresetId,
} from '@/lib/xray/inbound-presets';

const base: Omit<QuickInboundInput, 'preset'> = {
  remark: 'HK A',
  port: 443,
  nodeId: null,
  domain: 'hk.example.com',
  certFile: '/root/cert/hk.example.com/fullchain.pem',
  keyFile: '/root/cert/hk.example.com/privkey.pem',
  realityTarget: 'www.microsoft.com:443',
  realityServerNames: ['www.microsoft.com'],
  realityPrivateKey: 'cHJpdmF0ZS1rZXktZm9yLXRlc3RzLW9ubHktMDAwMDA',
  realityPublicKey: 'cHVibGljLWtleS1mb3ItdGVzdHMtb25seS0wMDAwMDA',
};

function wireOf(preset: QuickPresetId, over: Partial<QuickInboundInput> = {}) {
  const parsed = InboundFormSchema.safeParse(buildQuickInbound({ ...base, preset, ...over }));
  if (!parsed.success) throw new Error(`${preset}: ${JSON.stringify(parsed.error.issues)}`);
  const payload = formValuesToWirePayload(parsed.data);
  const json = (v: unknown) => (typeof v === 'string' ? JSON.parse(v) : v);
  return { payload, stream: json(payload.streamSettings), settings: json(payload.settings) };
}

// The dialog posts without the form, so every preset must pass the form's own validation.
describe('buildQuickInbound', () => {
  it.each(QUICK_PRESETS.map((p) => p.id))('%s passes form validation', (id) => {
    const { payload, stream } = wireOf(id);
    const preset = presetById(id);
    expect(payload.protocol).toBe(preset.protocol);
    expect(stream.network).toBe(preset.network);
    expect(stream.security).toBe(preset.security);
  });

  it('puts TLS presets on the panel certificate and names the domain in links', () => {
    for (const id of ['tls-vision', 'trojan-tls', 'ws-cdn', 'hysteria2'] as const) {
      const { payload, stream } = wireOf(id);
      expect(stream.tlsSettings.serverName).toBe('hk.example.com');
      expect(stream.tlsSettings.certificates[0].certificateFile).toBe(base.certFile);
      expect(stream.tlsSettings.certificates[0].keyFile).toBe(base.keyFile);
      expect(payload.shareAddrStrategy).toBe('custom');
      expect(payload.shareAddr).toBe('hk.example.com');
    }
  });

  it('builds the CDN preset as WebSocket over HTTP/1.1 TLS for the domain', () => {
    const { stream } = wireOf('ws-cdn');
    expect(stream.tlsSettings.alpn).toEqual(['http/1.1']);
    expect(stream.wsSettings.host).toBe('hk.example.com');
    expect(stream.wsSettings.path).toMatch(/^\/[a-z0-9]{10}$/);
  });

  it('builds Hysteria2 with h3 and a salamander password', () => {
    const { stream, settings } = wireOf('hysteria2');
    expect(settings.version).toBe(2);
    expect(stream.tlsSettings.alpn).toEqual(['h3']);
    expect(stream.finalmask.udp[0].type).toBe('salamander');
    expect(stream.finalmask.udp[0].settings.password).toMatch(/^[a-z0-9]{16}$/);
  });

  it('builds Shadowsocks 2022 on TCP and UDP with a server key', () => {
    const { settings } = wireOf('ss2022');
    expect(settings.method).toBe('2022-blake3-aes-256-gcm');
    expect(settings.network).toBe('tcp,udp');
    expect(settings.password.length).toBeGreaterThan(20);
  });

  it('builds REALITY presets with the scanned target, keys and short ids', () => {
    for (const id of ['reality-vision', 'xhttp-reality'] as const) {
      const { payload, stream } = wireOf(id, { nodeId: 3 });
      const r = stream.realitySettings;
      expect(r.target).toBe('www.microsoft.com:443');
      expect(r.privateKey).toBe(base.realityPrivateKey);
      expect(r.settings.publicKey).toBe(base.realityPublicKey);
      expect(r.shortIds.every((s: string) => /^[0-9a-f]+$/.test(s))).toBe(true);
      expect(payload.shareAddrStrategy).toBe('node');
    }
    expect(wireOf('xhttp-reality').stream.xhttpSettings.path).toMatch(/^\/[a-z0-9]{10}$/);
  });

  it('rejects a TLS preset without certificate files instead of saving a dead inbound', () => {
    const values = buildQuickInbound({ ...base, preset: 'tls-vision', certFile: '', keyFile: '' });
    expect(InboundFormSchema.safeParse(values).success).toBe(false);
  });
});

describe('findDuplicate', () => {
  const existing = [
    {
      id: 1,
      remark: 'HK A Reality',
      port: 443,
      protocol: 'vless',
      nodeId: 3,
      streamSettings: '{"network":"tcp","security":"reality"}',
    },
    { id: 2, remark: 'Hy', port: 443, protocol: 'hysteria', nodeId: null, streamSettings: {} },
  ];

  it('finds the same protocol, transport and security on the same server only', () => {
    expect(findDuplicate(presetById('reality-vision'), 3, existing)?.id).toBe(1);
    expect(findDuplicate(presetById('reality-vision'), null, existing)).toBeUndefined();
    expect(findDuplicate(presetById('xhttp-reality'), 3, existing)).toBeUndefined();
    expect(findDuplicate(presetById('hysteria2'), null, existing)?.id).toBe(2);
  });
});

describe('assignPorts', () => {
  it('gives each preset its own port, keeps CDN on Cloudflare ports and lets UDP reuse 443', () => {
    const ports = assignPorts(['ss2022', 'hysteria2', 'ws-cdn', 'reality-vision', 'tls-vision'], {
      tcp: [443, 8443, 2083, 31000, 32000],
      udp: [443, 40000],
    });
    expect(ports.get('tls-vision')).toBe(443);
    expect(ports.get('reality-vision')).toBe(8443);
    expect(ports.get('ws-cdn')).toBe(2083);
    expect(ports.get('hysteria2')).toBe(443);
    expect(ports.get('ss2022')).toBe(31000);
  });

  it('reports 0 when no Cloudflare port is left for the CDN preset', () => {
    expect(assignPorts(['ws-cdn'], { tcp: [31000], udp: [] }).get('ws-cdn')).toBe(0);
  });
});

describe('uniqueRemark', () => {
  it('numbers a remark that is already taken', () => {
    expect(uniqueRemark('US A', 'Reality', new Set())).toBe('US A Reality');
    expect(uniqueRemark('US A', 'Reality', new Set(['US A Reality', 'US A Reality 2']))).toBe(
      'US A Reality 3',
    );
  });
});
