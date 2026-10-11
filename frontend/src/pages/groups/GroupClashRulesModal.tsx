import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Form, Input, Modal, Radio, Select } from 'antd';
import { useQuery } from '@tanstack/react-query';

import { CLASH_ROUTING_PRESETS, presetOf } from '@/pages/settings/clashRoutingPresets';
import RoutingChoiceEditor from '@/pages/rule-sets/RoutingChoiceEditor';
import { EMPTY_CHOICE, type RoutingChoice, fetchRuleSets } from '@/pages/rule-sets/ruleSetApi';
import { keys } from '@/api/queryKeys';

const NO_TEMPLATE = '__none__';
const CUSTOM = '__custom__';

interface Props {
  open: boolean;
  groupName: string | null;
  initialRules: string;
  initialChoice: RoutingChoice;
  saving: boolean;
  onClose: () => void;
  onSubmit: (rules: string, choice: RoutingChoice) => Promise<boolean>;
}

function templateOf(rules: string): string {
  if (!rules.trim()) return NO_TEMPLATE;
  return presetOf(rules)?.key ?? CUSTOM;
}

export default function GroupClashRulesModal({
  open,
  groupName,
  initialRules,
  initialChoice,
  saving,
  onClose,
  onSubmit,
}: Props) {
  const { t } = useTranslation();
  const followsGlobal =
    !initialRules.trim() && initialChoice.ruleSets.length === 0 && !initialChoice.extraRules.trim();
  const [own, setOwn] = useState(!followsGlobal);
  const [template, setTemplate] = useState(() => templateOf(initialRules));
  const [custom, setCustom] = useState(() =>
    templateOf(initialRules) === CUSTOM ? initialRules : '',
  );
  const [choice, setChoice] = useState<RoutingChoice>(initialChoice);
  const setsQuery = useQuery({
    queryKey: keys.ruleSets.list(),
    queryFn: fetchRuleSets,
    enabled: open && own,
  });

  async function confirm() {
    if (!own) {
      if (await onSubmit('', EMPTY_CHOICE)) onClose();
      return;
    }
    let rules = '';
    if (template === CUSTOM) rules = custom.trim();
    else if (template !== NO_TEMPLATE) {
      rules = CLASH_ROUTING_PRESETS.find((p) => p.key === template)?.url ?? '';
    }
    if (await onSubmit(rules, choice)) onClose();
  }

  return (
    <Modal
      open={open}
      title={t('pages.groups.clashRulesTitle', { name: groupName ?? '' })}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      onCancel={onClose}
      onOk={confirm}
      width={own ? 760 : 520}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        title={t('pages.groups.clashRulesDesc')}
        style={{ marginBottom: 16 }}
      />
      <Form layout="vertical">
        <Form.Item>
          <Radio.Group
            value={own ? 'own' : 'global'}
            onChange={(e) => setOwn(e.target.value === 'own')}
            options={[
              { value: 'global', label: t('pages.groups.clashRulesFollowGlobal') },
              { value: 'own', label: t('pages.groups.clashRulesOwn') },
            ]}
          />
        </Form.Item>
        {own && (
          <>
            <Form.Item label={t('pages.settings.subClashPreset')}>
              <Select
                value={template}
                onChange={setTemplate}
                options={[
                  { value: NO_TEMPLATE, label: t('pages.groups.clashRulesNoTemplate') },
                  ...CLASH_ROUTING_PRESETS.map((p) => ({
                    value: p.key,
                    label: t(`pages.settings.subClashPreset_${p.key}`),
                  })),
                  { value: CUSTOM, label: t('pages.groups.clashRulesCustom') },
                ]}
              />
            </Form.Item>
            {template === CUSTOM && (
              <Form.Item label={t('pages.groups.clashRulesCustom')}>
                <Input.TextArea
                  value={custom}
                  onChange={(e) => setCustom(e.target.value)}
                  autoSize={{ minRows: 3, maxRows: 10 }}
                  placeholder={t('pages.groups.clashRulesCustomPlaceholder')}
                />
              </Form.Item>
            )}
          </>
        )}
      </Form>
      {own && (
        <RoutingChoiceEditor
          sets={setsQuery.data ?? []}
          loading={setsQuery.isFetching}
          value={choice}
          onChange={setChoice}
        />
      )}
    </Modal>
  );
}
