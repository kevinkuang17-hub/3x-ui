import { HttpUtil } from '@/utils';

export const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export type BasicPolicy = 'DIRECT' | 'PROXY' | 'REJECT';
export const ROUTING_POLICIES: BasicPolicy[] = ['DIRECT', 'PROXY', 'REJECT'];
// A destination is a basic policy or the name of a policy group in the one-click template.
export type RoutingPolicy = string;

export interface RuleSet {
  id: number;
  name: string;
  category: string;
  rules: string;
  defaultPolicy: BasicPolicy;
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

export interface GlobalRouting extends RoutingChoice {
  template: string;
  templateOn: boolean;
  clashEnabled: boolean;
}

export async function fetchGlobalRouting(): Promise<GlobalRouting> {
  const msg = await HttpUtil.get<GlobalRouting>('/panel/api/ruleSets/routing', undefined, {
    silent: true,
  });
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load routing');
  return {
    ...msg.obj,
    ruleSets: msg.obj.ruleSets ?? [],
    extraRules: msg.obj.extraRules ?? '',
  };
}

// Policy groups a template defines; the first one is the template's main node choice.
export async function fetchTemplateGroups(template: string): Promise<string[]> {
  if (!template.trim()) return [];
  const msg = await HttpUtil.get<string[]>(
    '/panel/api/ruleSets/groups',
    { template },
    {
      silent: true,
    },
  );
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to read the template');
  return msg.obj ?? [];
}

// Built-in sets whose traffic a template already has a dedicated group for.
const GROUP_HINTS: Record<string, string[]> = {
  ai: ['AI服务', '国外AI', 'ChatGPT'],
  teams: ['微软服务', 'Microsoft'],
  'cross-border': ['国外电商'],
};

// suggestPolicy is the destination a set starts with: the matching template group if there is one.
export function suggestPolicy(set: RuleSet, groups: string[]): RoutingPolicy {
  for (const hint of GROUP_HINTS[set.builtinKey] ?? []) {
    const match = groups.find((g) => g.includes(hint));
    if (match) return match;
  }
  return set.defaultPolicy;
}

export function ruleCount(rules: string): number {
  return rules.split('\n').filter((l) => {
    const v = l.trim();
    return v !== '' && !v.startsWith('#') && !v.startsWith('//');
  }).length;
}
