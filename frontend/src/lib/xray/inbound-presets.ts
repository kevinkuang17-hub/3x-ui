import { createDefaultInboundSettings } from '@/lib/xray/inbound-defaults';
import {
  createHysteriaTlsSettingsWithDefaultCert,
  createTlsSettingsWithDefaultCert,
} from '@/lib/xray/inbound-tls-defaults';
import { rawInboundToFormValues } from '@/lib/xray/inbound-form-adapter';
import { coerceInboundJsonField } from '@/models/dbinbound';
import { RealityStreamSettingsSchema } from '@/schemas/protocols/security/reality';
import {
  type HysteriaMasquerade,
  HysteriaStreamSettingsSchema,
} from '@/schemas/protocols/stream/hysteria';
import { TcpStreamSettingsSchema } from '@/schemas/protocols/stream/tcp';
import { WsStreamSettingsSchema } from '@/schemas/protocols/stream/ws';
import { XHttpStreamSettingsSchema } from '@/schemas/protocols/stream/xhttp';
import { SniffingSchema } from '@/schemas/primitives/sniffing';
import type { InboundFormValues, ShareAddrStrategy } from '@/schemas/forms/inbound-form';
import { RandomUtil } from '@/utils';

export type QuickPresetId =
  | 'tls-vision'
  | 'reality-vision'
  | 'xhttp-reality'
  | 'ws-cdn'
  | 'trojan-tls'
  | 'hysteria2'
  | 'ss2022';

export type QuickTransport = 'tcp' | 'udp' | 'both';

export interface QuickPreset {
  id: QuickPresetId;
  protocol: string;
  network: string;
  security: 'tls' | 'reality' | 'none';
  transport: QuickTransport;
  // Short name appended to the server name in the generated remark.
  label: string;
}

// Listed in the order ports are handed out, so the main protocols get 443 first.
export const QUICK_PRESETS: readonly QuickPreset[] = [
  {
    id: 'tls-vision',
    protocol: 'vless',
    network: 'tcp',
    security: 'tls',
    transport: 'tcp',
    label: 'TLS',
  },
  {
    id: 'reality-vision',
    protocol: 'vless',
    network: 'tcp',
    security: 'reality',
    transport: 'tcp',
    label: 'Reality',
  },
  {
    id: 'ws-cdn',
    protocol: 'vless',
    network: 'ws',
    security: 'tls',
    transport: 'tcp',
    label: 'CDN',
  },
  {
    id: 'xhttp-reality',
    protocol: 'vless',
    network: 'xhttp',
    security: 'reality',
    transport: 'tcp',
    label: 'XHTTP',
  },
  {
    id: 'trojan-tls',
    protocol: 'trojan',
    network: 'tcp',
    security: 'tls',
    transport: 'tcp',
    label: 'Trojan',
  },
  {
    id: 'hysteria2',
    protocol: 'hysteria',
    network: 'hysteria',
    security: 'tls',
    transport: 'udp',
    label: 'Hy2',
  },
  {
    id: 'ss2022',
    protocol: 'shadowsocks',
    network: 'tcp',
    security: 'none',
    transport: 'both',
    label: 'SS',
  },
];

// Cloudflare only proxies these HTTPS ports, so a CDN inbound must sit on one of them.
export const CDN_PORTS: readonly number[] = [443, 2053, 2083, 2087, 2096, 8443];

export function presetById(id: QuickPresetId): QuickPreset {
  return QUICK_PRESETS.find((p) => p.id === id)!;
}

export interface QuickServerFacts {
  domain: string;
  certOk: boolean;
  pointsHere: boolean;
  // The domain resolves, but to other addresses: typically a Cloudflare-proxied record.
  behindProxy: boolean;
}

// A TLS preset cannot work without a certificate for the domain; everything else is a warning.
export function presetBlocked(preset: QuickPreset, facts: QuickServerFacts): boolean {
  return preset.security === 'tls' && (!facts.domain || !facts.certOk);
}

export type PresetWarning = '' | 'needsDirectDns' | 'noCdnDetected';

export function presetWarning(preset: QuickPreset, facts: QuickServerFacts): PresetWarning {
  if (preset.security !== 'tls' || presetBlocked(preset, facts)) return '';
  if (preset.id === 'ws-cdn') return facts.behindProxy ? '' : 'noCdnDetected';
  return facts.pointsHere ? '' : 'needsDirectDns';
}

// Direct TLS when the domain points here, the CDN path when it is proxied, REALITY always as the
// domain-free fallback; Trojan and Shadowsocks only for clients that need them.
export function recommendedPresets(facts: QuickServerFacts): QuickPresetId[] {
  if (facts.domain && facts.certOk && facts.pointsHere)
    return ['tls-vision', 'reality-vision', 'hysteria2'];
  if (facts.domain && facts.certOk && facts.behindProxy)
    return ['ws-cdn', 'reality-vision', 'xhttp-reality'];
  return ['reality-vision', 'xhttp-reality'];
}

export interface ExistingInbound {
  id: number;
  remark: string;
  port: number;
  protocol: string;
  nodeId?: number | null;
  streamSettings?: unknown;
}

export function inboundSignature(protocol: string, network: string, security: string): string {
  return `${protocol}|${network}|${security}`;
}

function existingSignature(ib: ExistingInbound): string {
  const stream = coerceInboundJsonField(ib.streamSettings);
  if (ib.protocol === 'hysteria') return inboundSignature('hysteria', 'hysteria', 'tls');
  const network = typeof stream.network === 'string' && stream.network ? stream.network : 'tcp';
  const security =
    typeof stream.security === 'string' && stream.security ? stream.security : 'none';
  return inboundSignature(ib.protocol, network, security);
}

// The inbound on the same server that already serves this preset, which makes it a duplicate.
export function findDuplicate(
  preset: QuickPreset,
  nodeId: number | null,
  inbounds: readonly ExistingInbound[],
): ExistingInbound | undefined {
  const want = inboundSignature(preset.protocol, preset.network, preset.security);
  return inbounds.find((ib) => (ib.nodeId ?? null) === nodeId && existingSignature(ib) === want);
}

// "US A Reality", then "US A Reality 2" when that remark is taken anywhere.
export function uniqueRemark(base: string, label: string, taken: ReadonlySet<string>): string {
  const stem = [base.trim(), label].filter(Boolean).join(' ');
  if (!taken.has(stem)) return stem;
  for (let i = 2; ; i++) {
    const next = `${stem} ${i}`;
    if (!taken.has(next)) return next;
  }
}

export interface PortPools {
  tcp: readonly number[];
  udp: readonly number[];
}

// Hands each selected preset its own port: TCP and UDP pools are separate, a CDN preset only
// takes a Cloudflare port, and Shadowsocks takes a port neither side uses. 0 means none was left.
export function assignPorts(
  ids: readonly QuickPresetId[],
  pools: PortPools,
): Map<QuickPresetId, number> {
  const usedTcp = new Set<number>();
  const usedUdp = new Set<number>();
  const out = new Map<QuickPresetId, number>();
  for (const preset of QUICK_PRESETS) {
    if (!ids.includes(preset.id)) continue;
    const pick = (pool: readonly number[], ok: (p: number) => boolean) => pool.find(ok) ?? 0;
    let port = 0;
    if (preset.transport === 'udp') {
      port = pick(pools.udp, (p) => !usedUdp.has(p));
    } else if (preset.transport === 'both') {
      port = pick(pools.tcp, (p) => !usedTcp.has(p) && !usedUdp.has(p));
    } else {
      port = pick(
        pools.tcp,
        (p) => !usedTcp.has(p) && (preset.id !== 'ws-cdn' || CDN_PORTS.includes(p)),
      );
    }
    if (port) {
      if (preset.transport !== 'udp') usedTcp.add(port);
      if (preset.transport !== 'tcp') usedUdp.add(port);
    }
    out.set(preset.id, port);
  }
  return out;
}

export interface QuickInboundInput {
  preset: QuickPresetId;
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
  // Hysteria2 only: what a browser gets over HTTP/3 without the password, e.g. the panel's decoy.
  masquerade?: HysteriaMasquerade;
}

function withCert(tls: Record<string, unknown>, input: QuickInboundInput): Record<string, unknown> {
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

function randomPath(): string {
  return `/${RandomUtil.randomLowerAndNum(10)}`;
}

function streamFor(preset: QuickPreset, input: QuickInboundInput): Record<string, unknown> {
  switch (preset.id) {
    case 'hysteria2':
      return {
        network: 'hysteria',
        security: 'tls',
        hysteriaSettings: HysteriaStreamSettingsSchema.parse(
          input.masquerade ? { masquerade: input.masquerade } : {},
        ),
        tlsSettings: withCert(createHysteriaTlsSettingsWithDefaultCert(), input),
        finalmask: {
          tcp: [],
          udp: [{ type: 'salamander', settings: { password: RandomUtil.randomLowerAndNum(16) } }],
        },
      };
    case 'ws-cdn': {
      // Xray serves WebSocket over HTTP/1.1 only, so the handshake must not offer h2.
      const tls = withCert(createTlsSettingsWithDefaultCert(), input);
      tls.alpn = ['http/1.1'];
      return {
        network: 'ws',
        security: 'tls',
        wsSettings: WsStreamSettingsSchema.parse({ path: randomPath(), host: input.domain }),
        tlsSettings: tls,
      };
    }
    case 'xhttp-reality':
      return {
        network: 'xhttp',
        security: 'reality',
        xhttpSettings: XHttpStreamSettingsSchema.parse({ path: randomPath() }),
        realitySettings: realitySettings(input),
      };
    default: {
      const stream: Record<string, unknown> = {
        network: 'tcp',
        security: preset.security,
        tcpSettings: TcpStreamSettingsSchema.parse({ header: { type: 'none' } }),
      };
      if (preset.security === 'tls') {
        stream.tlsSettings = withCert(createTlsSettingsWithDefaultCert(), input);
      } else if (preset.security === 'reality') {
        stream.realitySettings = realitySettings(input);
      }
      return stream;
    }
  }
}

// Links must name the certificate's domain for TLS; the rest use the server's own address.
function shareAddress(
  preset: QuickPreset,
  input: QuickInboundInput,
): { strategy: ShareAddrStrategy; addr: string } {
  if (preset.security === 'tls') return { strategy: 'custom', addr: input.domain };
  return { strategy: input.nodeId != null ? 'node' : 'listen', addr: '' };
}

export function buildQuickInbound(input: QuickInboundInput): InboundFormValues {
  const preset = presetById(input.preset);
  const share = shareAddress(preset, input);
  return rawInboundToFormValues({
    remark: input.remark,
    protocol: preset.protocol,
    settings: createDefaultInboundSettings(preset.protocol) ?? undefined,
    streamSettings: streamFor(preset, input),
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
