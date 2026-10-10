// Remote Clash routing docs converted daily from Aethersailor/Custom_OpenClash_Rules (CC BY-SA 4.0).
const BASE = 'https://raw.githubusercontent.com/kevinkuang17-hub/pxc/main/rules/3x-ui/';

export interface ClashRoutingPreset {
  key: 'standard' | 'lite' | 'full' | 'gfw';
  url: string;
}

export const CLASH_ROUTING_PRESETS: ClashRoutingPreset[] = [
  { key: 'standard', url: `${BASE}standard.yaml` },
  { key: 'lite', url: `${BASE}lite.yaml` },
  { key: 'full', url: `${BASE}full.yaml` },
  { key: 'gfw', url: `${BASE}gfw.yaml` },
];

export function presetOf(rules: string): ClashRoutingPreset | undefined {
  const v = rules.trim();
  return CLASH_ROUTING_PRESETS.find((p) => p.url === v);
}
