import { HttpUtil } from '@/utils';

export const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export type RoutingPolicy = 'DIRECT' | 'PROXY' | 'REJECT';
export const ROUTING_POLICIES: RoutingPolicy[] = ['DIRECT', 'PROXY', 'REJECT'];

export interface RuleSet {
  id: number;
  name: string;
  category: string;
  rules: string;
  defaultPolicy: RoutingPolicy;
  builtinKey: string;
  edited: boolean;
  sortOrder: number;
}

export interface RuleSetPick {
  id: number;
  policy: RoutingPolicy;
}

export interface RoutingChoice {
  ruleSets: RuleSetPick[];
  extraRules: string;
}

export const EMPTY_CHOICE: RoutingChoice = { ruleSets: [], extraRules: '' };

export async function fetchRuleSets(): Promise<RuleSet[]> {
  const msg = await HttpUtil.get<RuleSet[]>('/panel/api/ruleSets/list', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load rule sets');
  return msg.obj ?? [];
}

export async function fetchGlobalRouting(): Promise<RoutingChoice> {
  const msg = await HttpUtil.get<RoutingChoice>('/panel/api/ruleSets/routing', undefined, {
    silent: true,
  });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load routing');
  return {
    ruleSets: msg.obj?.ruleSets ?? [],
    extraRules: msg.obj?.extraRules ?? '',
  };
}

export function ruleCount(rules: string): number {
  return rules.split('\n').filter((l) => {
    const v = l.trim();
    return v !== '' && !v.startsWith('#') && !v.startsWith('//');
  }).length;
}
