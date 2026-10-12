import { describe, expect, it } from 'vitest';

import { InboundFormSchema } from '@/schemas/forms/inbound-form';
import { formValuesToWirePayload } from '@/lib/xray/inbound-form-adapter';
import { buildQuickInbound, type QuickInboundInput } from '@/lib/xray/inbound-presets';

const base: QuickInboundInput = {
  kind: 'tls',
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

function wireOf(input: QuickInboundInput) {
  const parsed = InboundFormSchema.safeParse(buildQuickInbound(input));
  if (!parsed.success) throw new Error(JSON.stringify(parsed.error.issues));
  const payload = formValuesToWirePayload(parsed.data);
  const stream =
    typeof payload.streamSettings === 'string'
      ? JSON.parse(payload.streamSettings)
      : payload.streamSettings;
  return { payload, stream };
}

// The one-click dialog posts without the form, so the preset itself must pass the form's validation.
describe('buildQuickInbound', () => {
  it('builds a TLS inbound on the panel certificate whose links name the domain', () => {
    const { payload, stream } = wireOf(base);
    expect(payload.protocol).toBe('vless');
    expect(payload.port).toBe(443);
    expect(payload.remark).toBe('HK A');
    expect(stream.network).toBe('tcp');
    expect(stream.security).toBe('tls');
    expect(stream.tlsSettings.serverName).toBe('hk.example.com');
    expect(stream.tlsSettings.certificates).toHaveLength(1);
    expect(stream.tlsSettings.certificates[0].certificateFile).toBe(base.certFile);
    expect(stream.tlsSettings.certificates[0].keyFile).toBe(base.keyFile);
    expect(payload.shareAddrStrategy).toBe('custom');
    expect(payload.shareAddr).toBe('hk.example.com');
  });

  it('builds a REALITY inbound with the scanned target, keys and fresh short ids', () => {
    const { payload, stream } = wireOf({ ...base, kind: 'reality', nodeId: 3 });
    expect(stream.security).toBe('reality');
    expect(stream.tlsSettings).toBeUndefined();
    const r = stream.realitySettings;
    expect(r.target).toBe('www.microsoft.com:443');
    expect(r.serverNames).toEqual(['www.microsoft.com']);
    expect(r.privateKey).toBe(base.realityPrivateKey);
    expect(r.settings.publicKey).toBe(base.realityPublicKey);
    expect(r.shortIds.length).toBeGreaterThan(0);
    expect(r.shortIds.every((id: string) => /^[0-9a-f]+$/.test(id))).toBe(true);
    expect(payload.nodeId).toBe(3);
    expect(payload.shareAddrStrategy).toBe('node');
  });

  it('rejects a TLS preset without certificate files instead of saving a dead inbound', () => {
    const parsed = InboundFormSchema.safeParse(
      buildQuickInbound({ ...base, certFile: '', keyFile: '' }),
    );
    expect(parsed.success).toBe(false);
  });
});
