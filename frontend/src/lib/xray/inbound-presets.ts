import { createDefaultInboundSettings } from '@/lib/xray/inbound-defaults';
import { createTlsSettingsWithDefaultCert } from '@/lib/xray/inbound-tls-defaults';
import { rawInboundToFormValues } from '@/lib/xray/inbound-form-adapter';
import { RealityStreamSettingsSchema } from '@/schemas/protocols/security/reality';
import { TcpStreamSettingsSchema } from '@/schemas/protocols/stream/tcp';
import { SniffingSchema } from '@/schemas/primitives/sniffing';
import type { InboundFormValues, ShareAddrStrategy } from '@/schemas/forms/inbound-form';
import { RandomUtil } from '@/utils';

export type QuickInboundKind = 'tls' | 'reality';

export interface QuickInboundInput {
  kind: QuickInboundKind;
  remark: string;
  port: number;
  nodeId: number | null;
  domain: string;
  certFile: string;
  keyFile: string;
  realityTarget: string;
  realityServerNames: string[];
  realityPrivateKey: string;
  realityPublicKey: string;
}

function tlsSettings(input: QuickInboundInput): Record<string, unknown> {
  const tls = createTlsSettingsWithDefaultCert();
  tls.serverName = input.domain;
  const [cert] = tls.certificates as Record<string, unknown>[];
  tls.certificates = [{ ...cert, certificateFile: input.certFile, keyFile: input.keyFile }];
  return tls;
}

function realitySettings(input: QuickInboundInput): Record<string, unknown> {
  const reality = RealityStreamSettingsSchema.parse({}) as Record<string, unknown>;
  reality.target = input.realityTarget;
  reality.serverNames = input.realityServerNames;
  reality.privateKey = input.realityPrivateKey;
  reality.shortIds = RandomUtil.randomShortIds()
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  reality.settings = {
    ...(reality.settings as Record<string, unknown>),
    publicKey: input.realityPublicKey,
    spiderX: `/${RandomUtil.randomSeq(15)}`,
  };
  return reality;
}

// Links must name the certificate's domain for TLS; REALITY links use the server's own address.
function shareAddress(input: QuickInboundInput): { strategy: ShareAddrStrategy; addr: string } {
  if (input.kind === 'tls') return { strategy: 'custom', addr: input.domain };
  return { strategy: input.nodeId != null ? 'node' : 'listen', addr: '' };
}

// VLESS over raw TCP with TLS or REALITY: the pair Xray recommends for XTLS Vision clients.
export function buildQuickInbound(input: QuickInboundInput): InboundFormValues {
  const share = shareAddress(input);
  const streamSettings: Record<string, unknown> = {
    network: 'tcp',
    security: input.kind,
    tcpSettings: TcpStreamSettingsSchema.parse({ header: { type: 'none' } }),
  };
  if (input.kind === 'tls') streamSettings.tlsSettings = tlsSettings(input);
  else streamSettings.realitySettings = realitySettings(input);
  return rawInboundToFormValues({
    remark: input.remark,
    protocol: 'vless',
    settings: createDefaultInboundSettings('vless') ?? undefined,
    streamSettings,
    sniffing: SniffingSchema.parse({}),
    port: input.port,
    listen: '',
    tag: '',
    enable: true,
    trafficReset: 'never',
    nodeId: input.nodeId,
    shareAddrStrategy: share.strategy,
    shareAddr: share.addr,
  });
}
