import { z } from 'zod';

// AnyTLS and NaiveProxy run in the panel's sing-box sidecar. Clients log in with a password
// (NaiveProxy also sends the email as the user name); TLS comes from the stream's tlsSettings.
export const SidecarClientSchema = z.object({
  password: z.string().min(1),
  email: z.string().min(1),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  reset: z.number().int().min(0).default(0),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type SidecarClient = z.infer<typeof SidecarClientSchema>;

export const SidecarInboundSettingsSchema = z.object({
  clients: z.array(SidecarClientSchema).default([]),
});
export type SidecarInboundSettings = z.infer<typeof SidecarInboundSettingsSchema>;

export const SIDECAR_PROTOCOLS: readonly string[] = ['anytls', 'naive'];

export const isSidecarProtocol = (protocol: string) => SIDECAR_PROTOCOLS.includes(protocol);
