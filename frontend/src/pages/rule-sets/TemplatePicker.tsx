import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Form, Input, Select, Tag } from 'antd';
import { useQuery } from '@tanstack/react-query';

import { CLASH_ROUTING_PRESETS, presetOf } from '@/pages/settings/clashRoutingPresets';
import { keys } from '@/api/queryKeys';
import { fetchTemplateGroups } from './ruleSetApi';

const NONE = '__none__';
const CUSTOM = '__custom__';

function choiceOf(template: string): string {
  if (!template.trim()) return NONE;
  return presetOf(template)?.key ?? CUSTOM;
}

// useTemplateGroups reads the policy groups of a template; inline rule text is skipped until it is a URL.
export function useTemplateGroups(template: string) {
  const v = template.trim();
  return useQuery({
    queryKey: [...keys.ruleSets.root(), 'groups', v],
    queryFn: () => fetchTemplateGroups(v),
    enabled: v.startsWith('https://') || v.includes('proxy-groups'),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

interface Props {
  value: string;
  onChange: (template: string) => void;
  groups: string[];
  groupsError?: string;
}

// TemplatePicker chooses the one-click routing template the rule library builds on.
export default function TemplatePicker({ value, onChange, groups, groupsError }: Props) {
  const { t } = useTranslation();
  const [choice, setChoice] = useState(() => choiceOf(value));
  // Custom text is handed up when the box loses focus, so typing a URL does not fetch every prefix.
  const [customText, setCustomText] = useState(() => (choiceOf(value) === CUSTOM ? value : ''));
  return (
    <Form layout="vertical">
      <Form.Item label={t('pages.ruleSets.template')} extra={t('pages.ruleSets.templateDesc')}>
        <Select
          value={choice}
          onChange={(c: string) => {
            setChoice(c);
            if (c === NONE) onChange('');
            else if (c === CUSTOM) onChange(customText.trim());
            else onChange(CLASH_ROUTING_PRESETS.find((p) => p.key === c)?.url ?? '');
          }}
          options={[
            { value: NONE, label: t('pages.groups.clashRulesNoTemplate') },
            ...CLASH_ROUTING_PRESETS.map((p) => ({
              value: p.key,
              label: t(`pages.settings.subClashPreset_${p.key}`),
            })),
            { value: CUSTOM, label: t('pages.groups.clashRulesCustom') },
          ]}
        />
      </Form.Item>
      {choice === CUSTOM && (
        <Form.Item>
          <Input.TextArea
            value={customText}
            onChange={(e) => setCustomText(e.target.value)}
            onBlur={() => onChange(customText.trim())}
            autoSize={{ minRows: 2, maxRows: 8 }}
            placeholder={t('pages.groups.clashRulesCustomPlaceholder')}
          />
        </Form.Item>
      )}
      {groupsError && (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }} title={groupsError} />
      )}
      {groups.length > 0 && (
        <Form.Item label={t('pages.ruleSets.templateGroups', { count: groups.length })}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
            {groups.map((g, i) => (
              <Tag key={g} color={i === 0 ? 'blue' : undefined}>
                {g}
              </Tag>
            ))}
          </div>
        </Form.Item>
      )}
    </Form>
  );
}
