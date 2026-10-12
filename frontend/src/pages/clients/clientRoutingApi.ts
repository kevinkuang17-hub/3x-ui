import { HttpUtil } from '@/utils';
import { JSON_HEADERS, type RuleSetPick } from '@/pages/rule-sets/ruleSetApi';
import type { DnsConfig } from '@/pages/rule-sets/DnsEditor';

export type RoutingSource = 'client' | 'group' | 'global';

export interface ClientRouting {
  email: string;
  group: string;
  own: boolean;
  clashRules: string;
  ruleSets: RuleSetPick[];
  extraRules: string;
  ownDns: boolean;
  dns: DnsConfig | null;
  source: RoutingSource;
}

export const clientRoutingKey = (email: string) => ['ruleSets', 'client', email] as const;

export async function fetchClientRouting(email: string): Promise<ClientRouting> {
  const msg = await HttpUtil.get<ClientRouting>(
    `/panel/api/ruleSets/client?email=${encodeURIComponent(email)}`,
    undefined,
    { silent: true },
  );
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load routing');
  return { ...msg.obj, ruleSets: msg.obj.ruleSets ?? [], extraRules: msg.obj.extraRules ?? '' };
}

export interface ClientRoutingSave {
  email: string;
  own: boolean;
  clashRules: string;
  ruleSets: RuleSetPick[];
  extraRules: string;
  dns: DnsConfig | null;
}

export async function saveClientRouting(body: ClientRoutingSave): Promise<ClientRouting | null> {
  const msg = await HttpUtil.post<ClientRouting>('/panel/api/ruleSets/client', body, JSON_HEADERS);
  return msg?.success && msg.obj ? msg.obj : null;
}
